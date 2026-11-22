package system

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
)

func serverRows(values map[string]string) []*System {
	types := map[string]string{
		"NodePullInterval":       "int",
		"NodePushInterval":       "int",
		"TrafficReportThreshold": "int",
	}
	var rows []*System
	for key, value := range values {
		kind := types[key]
		if kind == "" {
			kind = "string"
		}
		rows = append(rows, &System{Category: "server", Key: key, Value: value, Type: kind})
	}
	return rows
}

func TestParseNodeConfig(t *testing.T) {
	parsed, err := ParseNodeConfig(serverRows(map[string]string{
		"NodeSecret":             "secret",
		"NodePullInterval":       "10",
		"NodePushInterval":       "60",
		"TrafficReportThreshold": "1024",
		"IPStrategy":             "prefer_ipv4",
		"DNS":                    `[{"proto":"udp","address":"1.1.1.1:53","domains":["example.com"]}]`,
		"Block":                  `["ads.example","","ads.example","tracker.example"]`,
		"Outbound":               `[{"name":"warp","protocol":"wireguard","address":"162.159.192.1","port":2408,"password":"k","rules":["geosite:openai"]}]`,
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := config.NodeConfig{
		NodeSecret:             "secret",
		NodePullInterval:       10,
		NodePushInterval:       60,
		TrafficReportThreshold: 1024,
		IPStrategy:             "prefer_ipv4",
		DNS:                    []config.NodeDNS{{Proto: "udp", Address: "1.1.1.1:53", Domains: []string{"example.com"}}},
		Block:                  []string{"ads.example", "tracker.example"},
		Outbound: []config.NodeOutbound{{
			Name: "warp", Protocol: "wireguard", Address: "162.159.192.1", Port: 2408,
			Password: "k", Rules: []string{"geosite:openai"},
		}},
	}
	if !reflect.DeepEqual(parsed, want) {
		t.Fatalf("parsed\n%+v\nwant\n%+v", parsed, want)
	}
}

// The rows a fresh installation seeds hold empty documents.
func TestParseNodeConfigEmptyDocuments(t *testing.T) {
	parsed, err := ParseNodeConfig(serverRows(map[string]string{"DNS": "", "Block": "", "Outbound": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.DNS != nil || parsed.Block != nil || parsed.Outbound != nil {
		t.Fatalf("empty documents decoded as %+v", parsed)
	}
	// An explicitly empty list stays a list: the admin console receives [].
	parsed, err = ParseNodeConfig(serverRows(map[string]string{"DNS": "[]", "Outbound": "[]"}))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(struct {
		DNS      []config.NodeDNS
		Outbound []config.NodeOutbound
	}{parsed.DNS, parsed.Outbound})
	if string(data) != `{"DNS":[],"Outbound":[]}` {
		t.Fatalf("empty lists encode as %s", data)
	}
}

func TestParseNodeConfigReportsMalformedDocuments(t *testing.T) {
	for _, key := range []string{"DNS", "Outbound"} {
		_, err := ParseNodeConfig(serverRows(map[string]string{key: `{not json`}))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("%s: err = %v, want a decode error naming the setting", key, err)
		}
		var syntax *json.SyntaxError
		if !errors.As(err, &syntax) {
			t.Fatalf("%s: err = %v, want the json.SyntaxError reachable", key, err)
		}
	}
}

// A malformed block list never failed the node configuration; it still
// does not, and whatever decodes is kept.
func TestParseNodeConfigBlockIsLenient(t *testing.T) {
	parsed, err := ParseNodeConfig(serverRows(map[string]string{"Block": `{not json`}))
	if err != nil || len(parsed.Block) != 0 {
		t.Fatalf("malformed block = %v (err %v), want it ignored", parsed.Block, err)
	}
	parsed, err = ParseNodeConfig(serverRows(map[string]string{"Block": `["a", 1, "b"]`}))
	if err != nil || !reflect.DeepEqual(parsed.Block, []string{"a", "b"}) {
		t.Fatalf("partially malformed block = %v (err %v), want [a b]", parsed.Block, err)
	}
}
