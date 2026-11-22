package render

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

// serverOnlyFields are the protocol settings a client never receives: the
// node's own listening port (clients use the node entry's), the switch, the
// plugin in its server form, the server secrets, the REALITY handshake
// target (clients connect to the node and send the SNI) and the DNS provider
// with the credentials the node's ACME client uses.
var serverOnlyFields = map[string]bool{
	"Port": true, "Enable": true, "Plugin": true, "PluginOptions": true,
	"RealityPrivateKey": true, "EncryptionTicket": true, "EncryptionServerPadding": true, "EncryptionPrivateKey": true,
	"RealityServerAddr": true, "RealityServerPort": true, "CertDNSProvider": true, "CertDNSEnv": true,
}

// renamedFields maps protocol fields to the proxy field templates read them
// by.
var renamedFields = map[string]string{"Cipher": "Method"}

// Every client-facing protocol setting reaches the proxy, under the name the
// templates use, and no server secret does: each string setting carries a
// marker the proxy must or must not show.
func TestNewProxyMapsEveryClientField(t *testing.T) {
	var protocol node.Protocol
	pv := reflect.ValueOf(&protocol).Elem()
	pt := pv.Type()
	for i := 0; i < pt.NumField(); i++ {
		if pv.Field(i).Kind() == reflect.String {
			pv.Field(i).SetString("F:" + pt.Field(i).Name)
		}
	}
	protocol.AllowInsecure = true
	protocol.CertPinSHA256 = "" // a pin would switch AllowInsecure off
	proxy := newProxy(&node.Node{Name: "n", Address: "a", Port: 1, Tags: "t"}, protocol)
	proxyValue := reflect.ValueOf(proxy)
	proxyText := fmt.Sprintf("%+v", proxy)

	for i := 0; i < pt.NumField(); i++ {
		name := pt.Field(i).Name
		if serverOnlyFields[name] {
			if marker := "F:" + name; pv.Field(i).Kind() == reflect.String && strings.Contains(proxyText, marker) {
				t.Errorf("server-only %s reaches the client proxy", name)
			}
			continue
		}
		target := name
		if renamed, ok := renamedFields[name]; ok {
			target = renamed
		}
		field := proxyValue.FieldByName(target)
		if !field.IsValid() {
			t.Errorf("protocol field %s has no proxy field: map it in newProxy or list it as server-only", name)
			continue
		}
		if pv.Field(i).Kind() == reflect.String && field.Interface() != pv.Field(i).Interface() {
			t.Errorf("proxy %s = %v, want the protocol's %s", target, field.Interface(), name)
		}
	}
	if !proxy.AllowInsecure {
		t.Error("AllowInsecure without a certificate pin was dropped")
	}
	protocol.CertPinSHA256 = "pin"
	if newProxy(&node.Node{}, protocol).AllowInsecure {
		t.Error("a pinned certificate still allows insecure connections")
	}
}

// A template may dump whole proxies (toJson, toPrettyJson, printf "%+v"), so
// the proxies handed to it must not carry the server's secrets or targets at
// all.
// The rendering takes the path a subscription fetch takes, from the stored
// protocol JSON, which is not normalized on read: a row written by an older
// version still holds every field it was given.
func TestTemplatesCannotReachServerOnlySettings(t *testing.T) {
	protocols, err := json.Marshal([]node.Protocol{{
		Type: "vless", Port: 443, Enable: true, Security: "reality", SNI: "cdn.example",
		RealityServerAddr: "dest.example", RealityServerPort: 8443, RealityPrivateKey: "REALITY-PRIVATE-KEY",
		RealityPublicKey: "reality-public-key", RealityShortId: "0123abcd",
		CertMode: "dns", CertDNSProvider: "cloudflare", CertDNSEnv: "CF_DNS_API_TOKEN=cloudflare-secret-token",
		Encryption: "mlkem768x25519plus", EncryptionTicket: "ENCRYPTION-TICKET", EncryptionPrivateKey: "ENCRYPTION-PRIVATE-KEY",
		EncryptionServerPadding: "SERVER-PADDING",
	}})
	if err != nil {
		t.Fatal(err)
	}
	servers := []*node.Node{{
		Id: 1, Name: "edge", Address: "edge.example", Port: 443, Protocol: "vless",
		Server: &node.Server{Id: 1, Protocols: string(protocols)},
	}}
	client, err := NewAdapter(
		`{{ range .Proxies }}{{ toJson . }}{{ "\n" }}{{ toPrettyJson . }}{{ "\n" }}{{ printf "%+v" . }}{{ end }}`,
		WithServers(servers), WithOutputFormat("text"),
	).Client()
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Build()
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(out)
	if !strings.Contains(rendered, "reality-public-key") || !strings.Contains(rendered, "cdn.example") {
		t.Fatalf("the client settings did not render:\n%s", rendered)
	}
	for _, secret := range []string{
		"dest.example", "8443", "REALITY-PRIVATE-KEY",
		"cloudflare", "CF_DNS_API_TOKEN", "cloudflare-secret-token",
		"ENCRYPTION-TICKET", "ENCRYPTION-PRIVATE-KEY", "SERVER-PADDING",
		"RealityServerAddr", "RealityServerPort", "CertDNSProvider", "CertDNSEnv",
	} {
		if strings.Contains(rendered, secret) {
			t.Errorf("template output carries the server-only %q:\n%s", secret, rendered)
		}
	}
}

func TestTemplateCacheParsesEachTextOnce(t *testing.T) {
	cache := newTemplateCache(2)
	first, err := cache.get("{{ .SiteName }}")
	if err != nil {
		t.Fatal(err)
	}
	again, err := cache.get("{{ .SiteName }}")
	if err != nil || again != first {
		t.Fatalf("second fetch parsed again: %p vs %p, %v", again, first, err)
	}
	if _, err := cache.get("{{ .Broken"); err == nil {
		t.Fatal("a broken template parsed")
	}
	if cache.order.Len() != 1 {
		t.Fatalf("a failed parse was cached: %d entries", cache.order.Len())
	}
	// The least recently used template leaves once the cache is full.
	if _, err := cache.get("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.get("{{ .SiteName }}"); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.get("c"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.entries["b"]; ok || cache.order.Len() != 2 {
		t.Fatalf("eviction kept %d entries, b present = %v", cache.order.Len(), ok)
	}
	if kept, _ := cache.get("{{ .SiteName }}"); kept != first {
		t.Fatal("the recently used template was evicted")
	}
}

// Concurrent fetches share parsed templates safely (run with -race).
func TestClientBuildConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := &Client{
				ClientTemplate: `{{ range .Proxies }}{{ .Name }}:{{ .Port }};{{ end }}`,
				OutputFormat:   "text",
				Proxies:        []Proxy{{Name: fmt.Sprint(i), Port: 443}},
			}
			out, err := client.Build()
			if err != nil || string(out) != fmt.Sprintf("%d:443;", i) {
				t.Errorf("build %d = %q, %v", i, out, err)
			}
		}(i)
	}
	wg.Wait()
}
