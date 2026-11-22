package node

import (
	"slices"
	"testing"
)

func TestProtocolMatchValuesCoversAliases(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		// hysteria2 is the alias node distribution normalizes to "hysteria";
		// matching the nodes.protocol column must accept both spellings.
		{"hysteria", []string{"hysteria", "hysteria2"}},
		{"hysteria2", []string{"hysteria", "hysteria2"}},
		{"HYSTERIA2", []string{"hysteria", "hysteria2"}},
		{"ssr", []string{"shadowsocksr", "ssr", "shadowsocks-r"}},
		{"shadowsocksr", []string{"shadowsocksr", "ssr", "shadowsocks-r"}},
		{"shadowsocks", []string{"shadowsocks"}},
		{"vless", []string{"vless"}},
	}
	for _, c := range cases {
		got := ProtocolMatchValues(c.raw)
		if !slices.Equal(got, c.want) {
			t.Errorf("ProtocolMatchValues(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
