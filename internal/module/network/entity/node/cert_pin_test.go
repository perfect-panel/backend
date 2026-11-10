package node

import (
	"strings"
	"testing"
)

const testCertPin = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"

func TestNormalizeCertPinSHA256(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"lowercase hex", testCertPin, testCertPin},
		{"uppercase hex", strings.ToUpper(testCertPin), testCertPin},
		{"surrounding whitespace", "  " + testCertPin + "\n", testCertPin},
		{"empty", "", ""},
		{"too short", testCertPin[:63], ""},
		{"too long", testCertPin + "0", ""},
		{"non-hex characters", strings.Replace(testCertPin, "9", "g", 1), ""},
		{"colon separated", strings.Replace(testCertPin, "d0", ":d", 1), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeCertPinSHA256(tc.raw); got != tc.want {
				t.Fatalf("NormalizeCertPinSHA256(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestApplyReportedCertPinUpdatesSelfSignedProtocol(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
		{Type: "trojan", Port: 444, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("vmess", strings.ToUpper(testCertPin))
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if protocols[0].CertPinSHA256 != testCertPin {
		t.Fatalf("vmess CertPinSHA256 = %q, want %q", protocols[0].CertPinSHA256, testCertPin)
	}
	if protocols[1].CertPinSHA256 != "" {
		t.Fatalf("trojan CertPinSHA256 = %q, want empty (other protocol types must not be touched)", protocols[1].CertPinSHA256)
	}

	// A repeated report with the same fingerprint must be a no-op so the
	// heartbeat does not invalidate the server cache every cycle.
	changed, err = server.ApplyReportedCertPin("vmess", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() repeat error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() repeat changed = true, want false")
	}
}

func TestApplyReportedCertPinMatchesProtocolTypeAliases(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "hysteria", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("hysteria2", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true for hysteria2 alias")
	}
}

func TestApplyReportedCertPinUpdatesNowhere(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{{
		Type: "nowhere", Port: 443, Version: 3, Enable: true, Security: "tls",
		Network: "mix", SNI: "nowhere.example", ALPN: []string{"nw3"}, CertMode: "self",
	}}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}

	changed, err := server.ApplyReportedCertPin("nowhere", strings.ToUpper(testCertPin))
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if !changed {
		t.Fatal("ApplyReportedCertPin() changed = false, want true")
	}
	protocols, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if len(protocols) != 1 || protocols[0].CertPinSHA256 != testCertPin {
		t.Fatalf("Nowhere CertPinSHA256 = %#v, want %q", protocols, testCertPin)
	}
}

func TestApplyReportedCertPinIgnoresNonSelfCertMode(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vless", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "dns"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	changed, err := server.ApplyReportedCertPin("vless", testCertPin)
	if err != nil {
		t.Fatalf("ApplyReportedCertPin() error = %v", err)
	}
	if changed {
		t.Fatal("ApplyReportedCertPin() changed = true, want false for cert_mode=dns")
	}
}

func TestApplyReportedCertPinIgnoresInvalidFingerprint(t *testing.T) {
	server := new(Server)
	if err := server.MarshalProtocols([]Protocol{
		{Type: "vmess", Port: 443, Enable: true, Security: "tls", SNI: "example.com", CertMode: "self"},
	}); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	for _, raw := range []string{"", "not-a-fingerprint", testCertPin[:32]} {
		changed, err := server.ApplyReportedCertPin("vmess", raw)
		if err != nil {
			t.Fatalf("ApplyReportedCertPin(%q) error = %v", raw, err)
		}
		if changed {
			t.Fatalf("ApplyReportedCertPin(%q) changed = true, want false", raw)
		}
	}
}
