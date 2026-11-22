package billingtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/billing/entity/payment"
)

// EPayV2 is a local HTTPS peer with independent merchant/platform RSA keys.
// Requests and replies use a small independent signer to catch SDK drift.
type EPayV2 struct {
	t        testing.TB
	Server   *httptest.Server
	merchant *rsa.PrivateKey
	platform *rsa.PrivateKey
}

func NewEPayV2(t testing.TB, handler http.HandlerFunc) *EPayV2 {
	t.Helper()
	merchant, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platform, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &EPayV2{t: t, merchant: merchant, platform: platform}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("pid") != "1001" || r.PostForm.Get("sign_type") != "RSA" {
			t.Error("invalid EPay V2 request method or fields")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		params := make(map[string]string)
		for key, values := range r.PostForm {
			if len(values) != 1 {
				t.Error("duplicate EPay request field")
			}
			params[key] = values[0]
		}
		digest := epayDigest(params)
		signature, err := base64.StdEncoding.DecodeString(params["sign"])
		if err != nil || rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, digest[:], signature) != nil {
			t.Error("EPay request failed merchant signature verification")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(p.Server.Close)
	return p
}

func (p *EPayV2) Config(mode, paymentType string) string {
	p.t.Helper()
	public, err := x509.MarshalPKIXPublicKey(&p.platform.PublicKey)
	if err != nil {
		p.t.Fatal(err)
	}
	config := payment.EPayConfig{
		Pid: "1001", Url: p.Server.URL, Type: paymentType, Version: "v2", CheckoutMode: mode,
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(p.merchant)})),
		PublicKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public})),
	}
	data, err := config.Marshal()
	if err != nil {
		p.t.Fatal(err)
	}
	return string(data)
}

// Sign does not overwrite caller-provided pid/timestamp, allowing mismatch tests.
func (p *EPayV2) Sign(params map[string]string) map[string]string {
	p.t.Helper()
	values := make(map[string]string, len(params)+4)
	for key, value := range params {
		values[key] = value
	}
	if _, ok := values["pid"]; !ok {
		values["pid"] = "1001"
	}
	if _, ok := values["timestamp"]; !ok {
		values["timestamp"] = strconv.FormatInt(time.Now().Unix(), 10)
	}
	values["sign_type"] = "RSA"
	digest := epayDigest(values)
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.platform, crypto.SHA256, digest[:])
	if err != nil {
		p.t.Fatal(err)
	}
	values["sign"] = base64.StdEncoding.EncodeToString(signature)
	return values
}

func (p *EPayV2) Reply(w http.ResponseWriter, fields map[string]any) {
	p.t.Helper()
	params := make(map[string]string, len(fields))
	for key, value := range fields {
		params[key] = fmt.Sprint(value)
	}
	signed := p.Sign(params)
	for _, key := range []string{"pid", "timestamp", "sign_type", "sign"} {
		fields[key] = signed[key]
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(fields); err != nil {
		p.t.Error(err)
	}
}

func epayDigest(params map[string]string) [32]byte {
	var keys []string
	for key, value := range params {
		if key != "sign" && key != "sign_type" && strings.Trim(value, " \t\n\r\x00\x0b") != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}
	return sha256.Sum256([]byte(strings.Join(parts, "&")))
}
