package nano

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Vectors from RFC 7693 and the xno-call-binding standard (BINDING-STANDARD.md).
const (
	burnAccount   = "nano_1111111111111111111111111111111111111111111111111111hifc8npp"
	vectorPrice   = "50000000000000000000000000000"
	vectorCommit  = "86e75a339b908cc926a478c1a0e5eb5dd6fc612c1d3251eb661faa78135c36cf"
	vectorRepAcct = "nano_33q9dassq66es6mcay83n5kypqgpzjikr9bkc9ope9xch1borfphgehj4ozj"

	hashA = "A1B2C3D4E5F60718293A4B5C6D7E8F90A1B2C3D4E5F60718293A4B5C6D7E8F90"
	hashB = "0F1E2D3C4B5A69788796A5B4C3D2E1F00F1E2D3C4B5A69788796A5B4C3D2E1F0"
)

func TestBlake2bVectors(t *testing.T) {
	cases := []struct {
		in   []byte
		size int
		want string
	}{
		{[]byte("abc"), 64, "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923"},
		{nil, 5, "7d64c5272e"},
		{seq(200), 32, "63c3d97a9f8894d5e043a707b0fee7f7ec4c049a23bbf1079df20b4165f9e22d"},
	}
	for _, c := range cases {
		if got := hex.EncodeToString(blake2bSum(c.in, c.size)); got != c.want {
			t.Errorf("blake2b(%d bytes, %d) = %s, want %s", len(c.in), c.size, got, c.want)
		}
	}
}

func seq(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func account(t *testing.T, fill byte) string {
	t.Helper()
	a, err := EncodeAccount(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAccountEncodeDecode(t *testing.T) {
	got, err := EncodeAccount(make([]byte, 32))
	if err != nil || got != burnAccount {
		t.Fatalf("EncodeAccount(zero) = %q, %v", got, err)
	}
	pub := bytes.Repeat([]byte{0xab}, 32)
	acct, _ := EncodeAccount(pub)
	back, err := DecodeAccount(acct)
	if err != nil || !bytes.Equal(back, pub) {
		t.Fatalf("round trip failed: %v", err)
	}
	if !SameAccount("xrb_"+acct[5:], acct) {
		t.Fatal("legacy prefix is not the same account")
	}
}

func TestAccountRejectsBadChecksumAndShape(t *testing.T) {
	bad := []string{
		burnAccount[:len(burnAccount)-1] + "q",    // checksum changed
		"nano_3" + burnAccount[6:],                // key changed, checksum kept
		"nano_" + strings.Repeat("1", 59),         // too short
		"btc_" + burnAccount[5:],                  // wrong prefix
		strings.Replace(burnAccount, "1", "0", 3), // 0 is not in the alphabet
		"nano_4" + burnAccount[6:],                // pad bits set
	}
	for _, a := range bad {
		if ValidateAccount(a) == nil {
			t.Errorf("ValidateAccount(%q) accepted", a)
		}
	}
}

func TestRawFromXNO(t *testing.T) {
	good := map[string]string{
		"1":                                "1000000000000000000000000000000",
		"0.05":                             vectorPrice,
		"0.001":                            "1000000000000000000000000000",
		".5":                               "500000000000000000000000000000",
		"0.000000000000000000000000000001": "1",
	}
	for in, want := range good {
		got, err := RawFromXNO(in)
		if err != nil || got.String() != want {
			t.Errorf("RawFromXNO(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", "0", "0.0", "-1", "1e3", "1.", "abc", "1,5", "0.0000000000000000000000000000001"} {
		if _, err := RawFromXNO(in); err == nil {
			t.Errorf("RawFromXNO(%q) accepted", in)
		}
	}
}

// The raw price is derived from the order's minor units and the operator's
// rate, and converts back exactly; nothing restates 10^30.
func TestRateConvertsMinorUnitsExactly(t *testing.T) {
	rate, err := ParseRate("2.5") // 2.5 XNO per currency unit
	if err != nil {
		t.Fatal(err)
	}
	one, _ := RawFromXNO("1")
	if want := new(big.Int).Mul(one, big.NewInt(5)); new(big.Int).Mul(rate.RawPerUnit(), big.NewInt(2)).Cmp(want) != 0 {
		t.Fatalf("rate = %s raw per unit", rate.RawPerUnit())
	}
	raw, err := rate.Raw(1990) // 19.90 units
	if err != nil {
		t.Fatal(err)
	}
	want, _ := RawFromXNO("49.75")
	if raw.Cmp(want) != 0 {
		t.Fatalf("Raw(1990) = %s, want %s", raw, want)
	}
	minor, err := rate.Minor(raw)
	if err != nil || minor != 1990 {
		t.Fatalf("Minor(Raw(1990)) = %d, %v", minor, err)
	}
	if _, err := rate.Minor(new(big.Int).Add(raw, big.NewInt(1))); err == nil {
		t.Fatal("a raw amount between two minor units was accepted")
	}
	for _, minor := range []int64{0, -5} {
		if _, err := rate.Raw(minor); err == nil {
			t.Errorf("Raw(%d) accepted", minor)
		}
	}
	// A rate finer than one raw per minor unit cannot price every order exactly.
	if _, err := ParseRate("0.000000000000000000000000000001"); err == nil {
		t.Fatal("a rate that is not whole raw per minor unit was accepted")
	}
	if _, err := ParseRate("0"); err == nil {
		t.Fatal("a zero rate was accepted")
	}
	if _, err := (Rate{}).Raw(100); err == nil {
		t.Fatal("an unset rate priced an order")
	}
}

func TestCommitmentVectors(t *testing.T) {
	c := Commitment("order-0001", burnAccount, vectorPrice)
	if hex.EncodeToString(c[:]) != vectorCommit {
		t.Fatalf("commitment = %x", c)
	}
	if got := CommitmentAccount("order-0001", burnAccount, vectorPrice); got != vectorRepAcct {
		t.Fatalf("commitment account = %s", got)
	}
}

func TestPaymentURI(t *testing.T) {
	uri := PaymentURI(burnAccount, vectorPrice, vectorRepAcct)
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "nano" || u.Opaque != burnAccount {
		t.Fatalf("PaymentURI = %q (%v)", uri, err)
	}
	if u.Query().Get("amount") != vectorPrice || u.Query().Get("representative") != vectorRepAcct {
		t.Fatalf("PaymentURI query = %q", u.RawQuery)
	}
}

func TestValidateNodeURL(t *testing.T) {
	for _, ok := range []string{"https://node.example/rpc", "http://127.0.0.1:7076", "http://localhost:7076", "http://[::1]:7076"} {
		if err := ValidateNodeURL(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "node.example", "http://node.example:7076", "ftp://node.example", "https://user:pw@node.example", "https://"} {
		if err := ValidateNodeURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// fakeNode answers block_info for the blocks it holds.
type fakeNode map[string]map[string]any

func (n fakeNode) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	_ = json.NewDecoder(r.Body).Decode(&req)
	b, ok := n[req["hash"]]
	if req["action"] != "block_info" || req["json_block"] != "true" || !ok {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Block not found"})
		return
	}
	_ = json.NewEncoder(w).Encode(b)
}

func sendBlock(payer, payTo, amount, rep string) map[string]any {
	return map[string]any{
		"block_account": payer,
		"amount":        amount,
		"confirmed":     "true",
		"subtype":       "send",
		"contents": map[string]string{
			"type":            "state",
			"representative":  rep,
			"link_as_account": payTo,
		},
	}
}

func TestNodeBlockInfo(t *testing.T) {
	payer, payTo := account(t, 0x11), account(t, 0x22)
	node := fakeNode{hashA: sendBlock(payer, payTo, vectorPrice, vectorRepAcct)}
	srv := httptest.NewServer(node)
	defer srv.Close()
	n := &Node{URL: srv.URL}

	b, err := n.BlockInfo(context.Background(), strings.ToLower(hashA))
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash != hashA || b.Account != payer || b.Amount.String() != vectorPrice || !b.Confirmed || !b.Send ||
		b.Representative != vectorRepAcct || b.Destination != payTo {
		t.Fatalf("block = %+v", b)
	}
	if _, err := n.BlockInfo(context.Background(), hashB); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("unknown block: %v", err)
	}
	if _, err := n.BlockInfo(context.Background(), "xyz"); !errors.Is(err, ErrInvalidBlockID) {
		t.Fatalf("bad hash: %v", err)
	}
	node[hashA]["amount"] = "-1"
	if _, err := n.BlockInfo(context.Background(), hashA); err == nil {
		t.Fatal("a malformed amount was accepted")
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte(" "), maxRPCResponse+10))
	}))
	defer huge.Close()
	if _, err := (&Node{URL: huge.URL}).BlockInfo(context.Background(), hashA); err == nil {
		t.Fatal("an oversized node reply was read")
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer down.Close()
	if _, err := (&Node{URL: down.URL}).BlockInfo(context.Background(), hashA); err == nil {
		t.Fatal("a failing node was trusted")
	}
}

func block(t *testing.T, payer, payTo string, amount *big.Int, rep string) *Block {
	t.Helper()
	return &Block{Hash: hashA, Account: payer, Amount: amount, Confirmed: true, Send: true, Representative: rep, Destination: payTo}
}

func TestVerifyAcceptsTheBoundExactPayment(t *testing.T) {
	payer, payTo := account(t, 0x11), account(t, 0x22)
	rate, _ := ParseRate("2.5")
	raw, _ := rate.Raw(1990)
	want := Expectation{OrderNo: "202611170001", PayTo: payTo, Raw: raw}
	rep := CommitmentAccount(want.OrderNo, payTo, raw.String())
	if err := Verify(block(t, payer, payTo, raw, rep), want); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// The legacy prefix names the same accounts.
	if err := Verify(block(t, payer, "xrb_"+payTo[5:], raw, "xrb_"+rep[5:]), want); err != nil {
		t.Fatalf("Verify with xrb_ accounts: %v", err)
	}
}

func TestVerifyRefusals(t *testing.T) {
	payer, payTo, other := account(t, 0x11), account(t, 0x22), account(t, 0x33)
	raw, _ := RawFromXNO("49.75")
	want := Expectation{OrderNo: "202611170001", PayTo: payTo, Raw: raw}
	rep := CommitmentAccount(want.OrderNo, payTo, raw.String())
	over := new(big.Int).Add(raw, big.NewInt(1))
	short := new(big.Int).Sub(raw, big.NewInt(1))

	unconfirmed := block(t, payer, payTo, raw, rep)
	unconfirmed.Confirmed = false
	receive := block(t, payer, payTo, raw, rep)
	receive.Send = false
	badPayer := block(t, payer[:len(payer)-1]+"1", payTo, raw, rep)

	cases := []struct {
		name  string
		block *Block
		want  Expectation
		err   error
	}{
		{"wrong destination", block(t, payer, other, raw, rep), want, ErrWrongPayTo},
		{"overpaid", block(t, payer, payTo, over, rep), want, ErrWrongAmount},
		{"underpaid", block(t, payer, payTo, short, rep), want, ErrWrongAmount},
		{"block for another order", block(t, payer, payTo, raw, rep), Expectation{OrderNo: "202611170002", PayTo: payTo, Raw: raw}, ErrNotBound},
		{"plain representative", block(t, payer, payTo, raw, payer), want, ErrNotBound},
		{"unconfirmed", unconfirmed, want, ErrNotConfirmed},
		{"not a send", receive, want, ErrNotConfirmed},
		{"no block", nil, want, ErrNotConfirmed},
		{"no price", block(t, payer, payTo, raw, rep), Expectation{OrderNo: want.OrderNo, PayTo: payTo}, ErrInvalidAmount},
		{"bad payer checksum", badPayer, want, ErrInvalidAccount},
	}
	for _, c := range cases {
		if err := Verify(c.block, c.want); !errors.Is(err, c.err) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.err)
		}
	}
}
