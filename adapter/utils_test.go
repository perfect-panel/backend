package adapter

import (
	"testing"

	"github.com/perfect-panel/server/internal/model/entity/node"
)

func TestAdapterProxy(t *testing.T) {
	servers := getServers()
	if len(servers) == 0 {
		t.Fatal("no servers found")
	}

	proxies, err := NewAdapter(tpl).Proxies(servers)
	if err != nil {
		t.Fatalf("failed to adapt servers: %v", err)
	}
	if len(proxies) != 2 {
		t.Fatalf("proxies len = %d, want 2", len(proxies))
	}
	if proxies[0].Name != "TestShadowSocks" || proxies[0].Type != "shadowsocks" {
		t.Fatalf("first proxy = %#v, want shadowsocks proxy", proxies[0])
	}
	if proxies[0].Method != "aes-256-gcm" {
		t.Fatalf("first proxy method = %q, want aes-256-gcm", proxies[0].Method)
	}
	if proxies[1].Name != "TestTrojan" || proxies[1].SNI != "tls.example.com" {
		t.Fatalf("second proxy = %#v, want trojan proxy with SNI", proxies[1])
	}
}

func getServers() []*node.Node {
	srv := &node.Server{
		Id:      1,
		Name:    "TestServer",
		Address: "example.com",
	}
	if err := srv.MarshalProtocols([]node.Protocol{
		{
			Type:   "shadowsocks",
			Port:   1234,
			Enable: true,
			Cipher: "aes-256-gcm",
		},
		{
			Type:      "trojan",
			Port:      443,
			Enable:    true,
			Security:  "tls",
			SNI:       "tls.example.com",
			Transport: "tcp",
		},
	}); err != nil {
		panic(err)
	}

	enabled := true
	return []*node.Node{
		{
			Id:       1,
			Name:     "TestShadowSocks",
			Tags:     "stable,asia",
			Port:     1234,
			Address:  "ss.example.com",
			ServerId: srv.Id,
			Server:   srv,
			Protocol: "shadowsocks",
			Enabled:  &enabled,
			Sort:     1,
		},
		{
			Id:       2,
			Name:     "TestTrojan",
			Tags:     "tls",
			Port:     443,
			Address:  "trojan.example.com",
			ServerId: srv.Id,
			Server:   srv,
			Protocol: "trojan",
			Enabled:  &enabled,
			Sort:     2,
		},
	}
}
