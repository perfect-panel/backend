package node

import (
	"strings"
	"testing"
)

func TestNowhereSharedKeyProjection(t *testing.T) {
	const expected = "550e8400e29b41d4a716446655440000"
	for _, credential := range []string{expected, strings.ToUpper(expected), "550e8400-e29b-41d4-a716-446655440000", "550E8400-E29B-41D4-A716-446655440000"} {
		got, err := NowhereSharedKey(credential)
		if err != nil || got != expected {
			t.Fatalf("key projection=%q %v", got, err)
		}
	}
	for _, credential := range []string{"", "arbitrary-password", strings.Repeat("0", 31), strings.Repeat("g", 32), "550e8400_e29b_41d4_a716_446655440000", "550e8400-e29b-41d4-a716-446655440000 ", "urn:uuid:550e8400-e29b-41d4-a716-446655440000"} {
		if _, err := NowhereSharedKey(credential); err == nil {
			t.Fatal("invalid credential was accepted")
		}
	}
}
func TestNowhereMorphSurvivesNormalizationAndDistribution(t *testing.T) {
	for input, want := range map[string]string{"": "morph", " MORPH ": "morph", " NONE ": "none"} {
		t.Run(input, func(t *testing.T) {
			p, err := NormalizeProtocolForStorage(Protocol{Type: "nowhere", Port: 443, Security: "tls", SNI: "nw.example", CertMode: "self", Enable: true, Obfs: input})
			if err != nil {
				t.Fatal(err)
			}
			if p.Obfs != want || p.Version != 3 || len(p.ALPN) != 1 || p.ALPN[0] != "nw3" {
				t.Fatalf("normalized=%+v", p)
			}
			list := SanitizeProtocolsForNodeDistribution([]Protocol{p})
			if len(list) != 1 || list[0].Obfs != want {
				t.Fatalf("distribution lost Morph choice: %+v", list)
			}
		})
	}
}
func TestStoredNowhereUpgradesWithoutChangingOtherProtocols(t *testing.T) {
	original := `[{"type":"nowhere","version":1,"port":443,"enable":true,"security":"tls","sni":"nw.example","cert_mode":"self","alpn":["now/1"]},{"type":"tuic","version":5,"alpn":["h3"]}]`
	server := &Server{Protocols: original}
	list, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Version != 3 || list[0].ALPN[0] != "nw3" || list[0].Obfs != "morph" {
		t.Fatalf("stored Nowhere=%+v", list[0])
	}
	if list[1].Version != 5 || list[1].ALPN[0] != "h3" || server.Protocols != original {
		t.Fatal("read rewrote unrelated or persisted data")
	}
	if err = server.MarshalProtocols(list); err != nil {
		t.Fatal(err)
	}
	again, err := server.UnmarshalProtocols()
	if err != nil || again[0].Version != 3 {
		t.Fatal("projected configuration did not round trip")
	}
	for _, version := range []int{2, 3, 4} {
		p := Protocol{Type: "nowhere", Version: version, ALPN: []string{"custom"}, Obfs: "none"}
		upgradeStoredNowhere(&p)
		if p.Version != version || p.ALPN[0] != "custom" || p.Obfs != "none" {
			t.Fatal("rewrote a non-legacy configuration")
		}
	}
}
