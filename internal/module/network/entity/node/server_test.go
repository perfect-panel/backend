package node

import (
	"reflect"
	"testing"
)

func TestServerProtocolRoundTripPreservesExtendedNodeFields(t *testing.T) {
	protocols := []Protocol{
		{
			Type: "shadowsocks", Port: 8388, Enable: true, Mode: "tcp_and_udp",
			Plugin: "shadow-tls", PluginOptions: map[string]any{"version": "3"},
		},
		{
			Type: "shadowsocksr", Port: 8389, Enable: true, Network: "tcp,udp",
			SSRProtocol: "auth_aes128_md5", ProtocolParam: "1000", Obfs: "tls1.2_ticket_auth", ObfsParam: "example.com",
		},
		{
			Type: "mieru", Port: 8390, Version: 2, Enable: true,
			TrafficPattern: "default", UserHintIsMandatory: true,
		},
		{Type: "naive", Port: 443, Enable: true, Network: "tcp"},
		{Type: "snell", Port: 444, Version: 5, Enable: true},
		{
			Type: "vless", Port: 445, Enable: true, ALPN: []string{"h2", "http/1.1"},
			Encryption: "mlkem768x25519plus", EncryptionMode: "native", EncryptionRtt: "0rtt",
			EncryptionTicket: "ticket", EncryptionServerPadding: "100-200", EncryptionPrivateKey: "key",
			EncryptionClientPadding: "50-100", EncryptionPassword: "password",
		},
		{Type: "vmess", Port: 446, Enable: true, XhttpMode: "packet-up", XhttpExtra: "/extra"},
		{Type: "trojan", Port: 447, Enable: true},
		{Type: "anytls", Port: 448, Enable: true, PaddingScheme: "stop=8"},
		{Type: "hysteria", Port: 449, Enable: true, QUICCongestionControl: "bbr", Heartbeat: 10},
		{Type: "tuic", Port: 450, Enable: true, QUICCongestionControl: "bbr"},
		{
			Type: "nowhere", Port: 451, Version: 3, Enable: true, Security: "tls", Network: "mix",
			SNI: "nowhere.example", ALPN: []string{"nw3"}, CertMode: "self", CertPinSHA256: testCertPin,
		},
	}
	server := new(Server)
	if err := server.MarshalProtocols(protocols); err != nil {
		t.Fatalf("MarshalProtocols() error = %v", err)
	}
	decoded, err := server.UnmarshalProtocols()
	if err != nil {
		t.Fatalf("UnmarshalProtocols() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, protocols) {
		t.Fatalf("protocol round trip mismatch:\ngot  %#v\nwant %#v", decoded, protocols)
	}
}
