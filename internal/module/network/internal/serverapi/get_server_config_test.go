package serverapi

import (
	"encoding/json"
	"testing"

	"github.com/perfect-panel/server/internal/module/network/entity/node"
)

// legacyShapes is the legacy server-config output for one fully set protocol
// of each type, as the endpoint has always rendered it: the shared security
// and transport blocks, AnyTLS alone with its padding scheme.
const legacyShapes = `{"anytls":{"port":443,"security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"pad","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"}},"hysteria":{"hop_interval":30,"hop_ports":"1-2","obfs_password":"obfs","port":443,"security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"}},"nowhere":null,"shadowsocks":{"method":"aes","port":443,"server_key":"a2V5"},"trojan":{"port":443,"security":"reality","security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"},"transport":"ws","transport_config":{"congestion_controller":"bbr","disable_sni":true,"host":"host","path":"/p","reduce_rtt":true,"service_name":"svc","udp_relay_mode":"native"}},"tuic":{"port":443,"security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"}},"vless":{"flow":"flow","port":443,"security":"reality","security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"},"transport":"ws","transport_config":{"congestion_controller":"bbr","disable_sni":true,"host":"host","path":"/p","reduce_rtt":true,"service_name":"svc","udp_relay_mode":"native"}},"vmess":{"port":443,"security":"reality","security_config":{"allow_insecure":true,"fingerprint":"chrome","padding_scheme":"","reality_mldsa65seed":"","reality_private_key":"priv","reality_public_key":"pub","reality_server_addr":"addr","reality_server_port":8443,"reality_short_id":"sid","sni":"sni"},"transport":"ws","transport_config":{"congestion_controller":"bbr","disable_sni":true,"host":"host","path":"/p","reduce_rtt":true,"service_name":"svc","udp_relay_mode":"native"}}}`

func TestCompatibleKeepsTheLegacyShapes(t *testing.T) {
	out := map[string]any{}
	for _, typ := range []string{ShadowSocks, Vless, Vmess, Trojan, AnyTLS, Tuic, Hysteria, Nowhere} {
		out[typ] = compatible(node.Protocol{
			Type: typ, Port: 443, Enable: true, Security: "reality", SNI: "sni", AllowInsecure: true, Fingerprint: "chrome",
			RealityServerAddr: "addr", RealityServerPort: 8443, RealityPrivateKey: "priv", RealityPublicKey: "pub", RealityShortId: "sid",
			Transport: "ws", Host: "host", Path: "/p", ServiceName: "svc", Cipher: "aes", ServerKey: "key", Flow: "flow",
			DisableSNI: true, ReduceRtt: true, UDPRelayMode: "native", CongestionController: "bbr", PaddingScheme: "pad",
			HopPorts: "1-2", HopInterval: 30, ObfsPassword: "obfs",
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != legacyShapes {
		t.Fatalf("legacy server config shapes changed:\n%s\nwant\n%s", data, legacyShapes)
	}
}

func TestCompatibleDoesNotInventLegacyNowhereContract(t *testing.T) {
	if config := compatible(node.Protocol{
		Type: Nowhere, Port: 443, Version: 1, Enable: true, Security: "tls",
		Network: "mix", SNI: "node.example", ALPN: []string{"nw3"}, CertMode: "self",
	}); config != nil {
		t.Fatalf("compatible() = %#v, want nil so callers direct Nowhere nodes to /v2/server/{server_id}", config)
	}
}
