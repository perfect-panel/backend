package protocolmap

import (
	"reflect"
	"strings"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/network/contract"
	"github.com/perfect-panel/backend/internal/module/network/entity/node"
)

func jsonNames(t reflect.Type) map[string]string {
	names := make(map[string]string, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		names[name] = field.Type.String()
	}
	return names
}

// The mapping goes through the JSON names, so both forms must agree on
// every name and type; the certificate pin is the one stored-only field.
func TestProtocolFormsShareTheirJSONNames(t *testing.T) {
	stored, shown := jsonNames(reflect.TypeOf(node.Protocol{})), jsonNames(reflect.TypeOf(dto.Protocol{}))
	for name, typ := range shown {
		if stored[name] != typ {
			t.Errorf("API field %s (%s) has stored type %q", name, typ, stored[name])
		}
	}
	for name := range stored {
		if _, ok := shown[name]; !ok && name != "cert_pin_sha256" {
			t.Errorf("stored field %s has no API form", name)
		}
	}
}

func TestMappingKeepsEverySharedField(t *testing.T) {
	stored := node.Protocol{
		Type: "vless", Port: 443, Enable: true, Security: "reality", SNI: "example.com", ALPN: []string{"h2"},
		RealityPrivateKey: "private", RealityShortId: "abcd", Transport: "grpc", ServiceName: "svc", Flow: "xtls-rprx-vision",
		PluginOptions: map[string]any{"mode": "tls"}, Ratio: 1.5, CertMode: "dns", CertDNSEnv: "TOKEN=x", CertPinSHA256: "pin",
		EncryptionTicket: "ticket", UoTVersion: 2,
	}
	shown, err := ToDTO([]node.Protocol{stored})
	if err != nil || len(shown) != 1 {
		t.Fatalf("ToDTO = %v, %v", shown, err)
	}
	back, err := FromDTO(shown[0])
	if err != nil {
		t.Fatal(err)
	}
	want := stored
	want.CertPinSHA256 = ""
	if !reflect.DeepEqual(back, want) {
		t.Fatalf("round trip = %+v\nwant %+v", back, want)
	}
}
