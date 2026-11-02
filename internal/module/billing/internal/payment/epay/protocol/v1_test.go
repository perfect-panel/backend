package protocol

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
)

const v1TestKey = "test-v1-key+&="

func v1TestClient(t *testing.T) *V1Client {
	t.Helper()
	c, err := NewV1Client(V1Config{BaseURL: "https://pay.example.com", MerchantID: "1001", Key: v1TestKey})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Independent fixture signer. The exact canonicalization is also checked against
// a fixed digest generated with Python hashlib, including UTF-8 and whitespace.
func v1TestSignature(params url.Values) string {
	var keys, parts []string
	for key := range params {
		if key != "sign" && key != "sign_type" && params.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+params.Get(key))
	}
	digest := md5.Sum([]byte(strings.Join(parts, "&") + v1TestKey))
	return hex.EncodeToString(digest[:])
}

func v1TestNotification(changes Params) url.Values {
	values := url.Values{
		"pid": {"1001"}, "trade_no": {"platform-1"}, "out_trade_no": {"order-1"},
		"type": {"alipay"}, "name": {"会员 & 服务"}, "money": {"1.00"},
		"trade_status": {"TRADE_SUCCESS"}, "sign_type": {"MD5"}, "future_zero": {"0"},
	}
	for key, value := range changes {
		values.Set(key, value)
	}
	values.Set("sign", v1TestSignature(values))
	return values
}

func TestV1CanonicalizationAndGoldenSignature(t *testing.T) {
	params := Params{"name": " 会员 A&B=+ ", "money": "1.00", "blank": " \t", "zero": "0", "empty": "", "sign": "ignored", "sign_type": "MD5"}
	canonical, err := V1CanonicalString(params)
	if err != nil || canonical != "blank= \t&money=1.00&name= 会员 A&B=+ &zero=0" {
		t.Fatalf("canonical=%q err=%v", canonical, err)
	}
	signature, err := v1Signature("test-v1-key", params)
	if err != nil || signature != "d628ba977e11582794b8e4b146f67890" {
		t.Fatalf("signature=%s err=%v", signature, err)
	}
	v2, err := CanonicalString(params)
	if err != nil || strings.Contains(v2, "blank=") || v2 == canonical {
		t.Fatal("V1 whitespace handling must remain distinct from V2")
	}
	c := v1TestClient(t)
	signed, err := c.SignedParams(Params{"money": "1.00"})
	if err != nil {
		t.Fatal(err)
	}
	if signed["pid"] != "1001" || signed["sign_type"] != "MD5" || signed["sign"] != v1TestSignature(v1Values(signed)) {
		t.Fatal("invalid signed fields")
	}
	if _, exists := signed["timestamp"]; exists {
		t.Fatal("V1 must not inject V2 timestamp")
	}
	if _, exists := signed["key"]; exists {
		t.Fatal("page payment leaked merchant KEY")
	}
	for _, key := range []string{"key", "pid", "sign", "sign_type"} {
		if _, err := c.SignedParams(Params{key: "override"}); err == nil {
			t.Fatalf("reserved %s accepted", key)
		}
	}
}

func TestV1NotificationVerification(t *testing.T) {
	c := v1TestClient(t)
	values := v1TestNotification(nil)
	for i := 0; i < 2; i++ {
		notice, err := c.VerifyNotification(values)
		if err != nil || notice.OutTradeNo != "order-1" {
			t.Fatalf("valid callback without timestamp: %v", err)
		}
	}
	if _, err := c.VerifyNotification(v1TestNotification(Params{"timestamp": "1000000000"})); err != nil {
		t.Fatalf("optional extension is signed, not a V2 age requirement: %v", err)
	}
	for name, changes := range map[string]Params{
		"merchant": {"pid": "1002"}, "missing order": {"out_trade_no": ""},
		"status": {"trade_status": "WAIT_BUYER_PAY"}, "zero amount": {"money": "0"},
		"fraction": {"money": "1.001"}, "version": {"sign_type": "RSA"}, "missing name": {"name": ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.VerifyNotification(v1TestNotification(changes)); err == nil {
				t.Fatal("invalid notification accepted")
			}
		})
	}
	for _, mutate := range []func(url.Values){
		func(v url.Values) { v.Set("money", "2.00") },
		func(v url.Values) { v.Add("money", "1.00") },
		func(v url.Values) { v.Del("future_zero") },
		func(v url.Values) { v.Set("sign", strings.ToUpper(v.Get("sign"))) },
	} {
		values := v1TestNotification(nil)
		mutate(values)
		if _, err := c.VerifyNotification(values); err == nil {
			t.Fatal("tampered/ambiguous signature accepted")
		}
	}
}

func TestV1OperationSpecificSuccessCodes(t *testing.T) {
	c := v1TestClient(t)
	for _, tc := range []struct {
		body    string
		success int
		wantOK  bool
	}{
		{`{"code":1}`, 1, true}, {`{"code":"1"}`, 1, true}, {`{"code":0}`, 1, false},
		{`{"code":0}`, 0, true}, {`{"code":"0"}`, 0, true}, {`{"code":1}`, 0, false},
		{`{"code":-1}`, 1, false}, {`{}`, 1, false}, {`{"code":null}`, 1, false},
		{`{"code":true}`, 1, false}, {`{"code":1.0}`, 1, false}, {`{"code":"1.0"}`, 1, false},
		{`{"code":1,"code":0}`, 1, false}, {`{"code":1}{}`, 1, false},
	} {
		response, err := c.parseResponse([]byte(tc.body), tc.success)
		if (err == nil) != tc.wantOK || (response != nil) != tc.wantOK {
			t.Fatalf("body=%s success=%d err=%v", tc.body, tc.success, err)
		}
	}
	_, err := c.parseResponse([]byte(`{"code":0}`), 1)
	var apiError *V1APIError
	if !errors.As(err, &apiError) || apiError.Code != 0 {
		t.Fatalf("code 0 must fail ordinary requests: %v", err)
	}
}

type v1HTTPFixture struct {
	badRequest atomic.Value
	calls      atomic.Int32
	mode       atomic.Int32
}

func (p *v1HTTPFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.calls.Add(1)
	if err := r.ParseForm(); err != nil {
		p.badRequest.Store(err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var result any
	switch r.URL.Path {
	case "/mapi.php":
		result = p.create(r)
	case "/api.php":
		result = p.queryOrRefund(r)
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if result == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (p *v1HTTPFixture) create(r *http.Request) any {
	if r.Method != http.MethodPost || r.PostForm.Get("pid") != "1001" || r.PostForm.Get("sign_type") != "MD5" || r.PostForm.Get("sign") != v1TestSignature(r.PostForm) || r.PostForm.Has("key") || r.PostForm.Has("timestamp") || r.PostForm.Get("param") != "A+B&=中文" {
		p.badRequest.Store("invalid signed payment form")
	}
	if p.mode.Load() == 4 {
		return map[string]any{"code": 0, "msg": "failed"}
	}
	return map[string]any{"code": 1, "trade_no": "platform-1", "payurl": "https://pay.example.com/checkout"}
}

func (p *v1HTTPFixture) queryOrRefund(r *http.Request) any {
	if r.URL.Query().Get("act") == "refund" {
		return p.refund(r)
	}
	if r.Method != http.MethodGet || r.URL.Query().Get("key") != v1TestKey || r.URL.Query().Get("pid") != "1001" || r.URL.Query().Has("sign") {
		p.badRequest.Store("invalid query credentials")
	}
	switch r.URL.Query().Get("act") {
	case "order":
		result := map[string]any{"code": "1", "pid": "1001", "trade_no": "platform-1", "out_trade_no": "order-1", "type": "alipay", "money": "1.00", "status": "1"}
		if p.mode.Load() == 1 {
			result["out_trade_no"] = "wrong-order"
		}
		if p.mode.Load() == 2 {
			delete(result, "status")
		}
		return result
	case "query":
		return map[string]any{"code": 1, "pid": 1001, "money": "5.00", "key": v1TestKey}
	case "settle":
		return map[string]any{"code": 1, "data": []any{}}
	case "orders":
		if r.Form.Get("page") != "1" || r.Form.Get("limit") != "20" {
			p.badRequest.Store("wrong V1 pagination defaults")
		}
		return map[string]any{"code": 1, "data": []any{}}
	default:
		return nil
	}
}

func (p *v1HTTPFixture) refund(r *http.Request) any {
	if r.Method != http.MethodPost || r.URL.Query().Has("key") || r.PostForm.Get("key") != v1TestKey || r.PostForm.Get("pid") != "1001" || r.PostForm.Get("money") != "1.00" || r.PostForm.Get("out_trade_no") != "order-1" || r.PostForm.Has("out_refund_no") {
		p.badRequest.Store("invalid refund method/credentials/body")
	}
	code := 0
	if p.mode.Load() == 3 {
		code = 1
	}
	return map[string]any{"code": code, "msg": "refund result"}
}

func TestV1HTTPFlows(t *testing.T) {
	peer := &v1HTTPFixture{}
	server := httptest.NewTLSServer(peer)
	defer server.Close()
	c, err := NewV1Client(V1Config{BaseURL: server.URL, MerchantID: "1001", Key: v1TestKey, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	payment := V1Payment{Type: "alipay", OutTradeNo: "order-1", NotifyURL: "https://merchant.example/notify", ReturnURL: "https://merchant.example/return", Name: "会员 & 服务", Money: "1.00", Param: "A+B&=中文"}
	form, err := c.PagePayment(payment)
	if err != nil || form.Action != server.URL+"/submit.php" || form.Fields.Has("key") || form.Fields.Get("sign") != v1TestSignature(form.Fields) {
		t.Fatalf("page: %v", err)
	}
	created, err := c.CreatePayment(ctx, V1CreatePaymentRequest{V1Payment: payment, ClientIP: "127.0.0.1", Device: "jump"})
	if err != nil || created.PayURL == "" {
		t.Fatalf("create: %v", err)
	}
	ref := OrderRef{OutTradeNo: "order-1"}
	order, err := c.QueryOrder(ctx, ref)
	if err != nil || order.Status != 1 || order.MerchantID != "1001" {
		t.Fatalf("query: %v", err)
	}
	for _, call := range []func(context.Context) (*V1Response, error){c.MerchantInfo, c.Settlements, func(ctx context.Context) (*V1Response, error) { return c.Orders(ctx, V1OrdersRequest{}) }} {
		if _, err := call(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refund := V1RefundRequest{OrderRef: ref, Money: "1.00"}
	if _, err := c.Refund(ctx, refund); err != nil {
		t.Fatal(err)
	}
	for _, m := range []int32{1, 2} {
		peer.mode.Store(m)
		if _, err := c.QueryOrder(ctx, ref); err == nil {
			t.Fatal("mismatched/incomplete order accepted")
		}
	}
	peer.mode.Store(3)
	if _, err := c.Refund(ctx, refund); err == nil {
		t.Fatal("refund code 1 treated as success")
	}
	peer.mode.Store(4)
	if _, err := c.CreatePayment(ctx, V1CreatePaymentRequest{V1Payment: payment, ClientIP: "127.0.0.1"}); err == nil {
		t.Fatal("create code 0 treated as success")
	}
	before := peer.calls.Load()
	if _, err := c.Refund(ctx, V1RefundRequest{OrderRef: ref, Money: "0"}); err == nil {
		t.Fatal("zero refund accepted")
	}
	if _, err := c.Orders(ctx, V1OrdersRequest{Limit: 51}); err == nil {
		t.Fatal("invalid page limit accepted")
	}
	if _, err := c.QueryOrder(ctx, OrderRef{}); err == nil {
		t.Fatal("empty order ref accepted")
	}
	if peer.calls.Load() != before {
		t.Fatal("invalid input reached server")
	}
	if issue := peer.badRequest.Load(); issue != nil {
		t.Fatal(issue)
	}
}

type v1RoundTripper func(*http.Request) (*http.Response, error)

func (f v1RoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestV1NoRetriesRedirectsOrPrintableKeyLeaks(t *testing.T) {
	var calls atomic.Int32
	httpClient := &http.Client{Transport: v1RoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, &url.Error{Op: "Get", URL: r.URL.String(), Err: context.DeadlineExceeded}
	})}
	c, err := NewV1Client(V1Config{BaseURL: "https://pay.example.com", MerchantID: "1001", Key: v1TestKey, HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.QueryOrder(context.Background(), OrderRef{OutTradeNo: "order-1"})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), v1TestKey) || strings.Contains(err.Error(), url.QueryEscape(v1TestKey)) || calls.Load() != 1 {
		t.Fatalf("transport failure: %v calls=%d", err, calls.Load())
	}
	if httpClient.CheckRedirect != nil || httpClient.Timeout != 0 {
		t.Fatal("caller HTTP client was mutated")
	}
	body, _ := json.Marshal(map[string]any{"code": -1, "msg": "rejected key " + v1TestKey})
	if _, err := c.parseResponse(body, 1); err == nil || strings.Contains(err.Error(), v1TestKey) {
		t.Fatal("API error leaked key")
	}
	var followed atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			followed.Add(1)
			w.WriteHeader(500)
			return
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	c, err = NewV1Client(V1Config{BaseURL: server.URL, MerchantID: "1001", Key: v1TestKey, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.QueryOrder(context.Background(), OrderRef{OutTradeNo: "order-1"})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTemporaryRedirect || followed.Load() != 0 {
		t.Fatalf("redirect: %v", err)
	}
}

func TestV1ConfigurationAndOrderNumbers(t *testing.T) {
	for _, cfg := range []V1Config{
		{MerchantID: "1001", Key: v1TestKey},
		{BaseURL: "http://pay.example.com", MerchantID: "1001", Key: v1TestKey},
		{BaseURL: "https://pay.example.com", MerchantID: "1001"},
		{BaseURL: "https://pay.example.com", MerchantID: "invalid", Key: v1TestKey},
	} {
		if _, err := NewV1Client(cfg); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	for _, status := range []string{`1`, `"1"`} {
		var order V1Order
		data := fmt.Sprintf(`{"pid":1001,"status":%s,"trade_no":"123456789012345678901","money":"1.00"}`, status)
		if err := json.Unmarshal([]byte(data), &order); err != nil || order.TradeNo != "123456789012345678901" || order.Status != 1 {
			t.Fatalf("order integer/string handling: %v", err)
		}
	}
}

func TestV1PaymentEntryVariants(t *testing.T) {
	var body atomic.Value
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer server.Close()
	c, err := NewV1Client(V1Config{BaseURL: server.URL, MerchantID: "1001", Key: v1TestKey, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	request := V1CreatePaymentRequest{
		V1Payment: V1Payment{Type: "bank", OutTradeNo: "order-1", Name: "test", Money: "1.00", NotifyURL: "https://merchant.example/notify"},
		ClientIP:  "127.0.0.1",
	}
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"code":1,"trade_no":"p-1","payurl":"https://pay.example.com/1"}`, true},
		{`{"code":1,"trade_no":"p-1","qrcode":"weixin://pay/example"}`, true},
		{`{"code":1,"trade_no":"p-1","urlscheme":"weixin://example"}`, true},
		{`{"code":1,"trade_no":"p-1"}`, false},
		{`{"code":1,"payurl":"https://pay.example.com/1"}`, false},
		{`{"code":1,"trade_no":"p-1","payurl":"url","qrcode":"qr"}`, false},
	} {
		body.Store(tc.body)
		_, err := c.CreatePayment(context.Background(), request)
		if (err == nil) != tc.ok {
			t.Fatalf("response=%s err=%v", tc.body, err)
		}
	}
}

func TestV1ClientAcceptsDeploymentPath(t *testing.T) {
	for base, want := range map[string]string{
		"https://pay.example.com":        "https://pay.example.com",
		"https://pay.example.com/":       "https://pay.example.com",
		"https://pay.example.com/pay":    "https://pay.example.com/pay",
		"https://pay.example.com/a/pay/": "https://pay.example.com/a/pay",
	} {
		c, err := NewV1Client(V1Config{BaseURL: base, MerchantID: "1001", Key: v1TestKey})
		if err != nil || c.baseURL != want {
			t.Fatalf("%s: base=%v err=%v", base, c, err)
		}
	}
	for _, base := range []string{
		"http://pay.example.com/pay", "https://pay.example.com/pay?x=1", "https://pay.example.com/pay#f",
		"https://user@pay.example.com/pay", "https://pay.example.com/a/../pay", "https://pay.example.com/a//pay",
		"https://pay.example.com/p%2Fay", "https://pay.example.com/submit.php", "https://pay.example.com/pay/API.PHP/",
	} {
		if _, err := NewV1Client(V1Config{BaseURL: base, MerchantID: "1001", Key: v1TestKey}); err == nil {
			t.Fatalf("invalid base URL accepted: %q", base)
		}
	}
}
