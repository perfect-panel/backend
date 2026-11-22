package geoip

import (
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/requestmeta"
)

func TestIPLocationEnrichIgnoresInvalidAndPrivateAddresses(t *testing.T) {
	resolver := &IPLocation{}
	for _, address := range []string{"", "not-an-ip", "127.0.0.1", "10.0.0.1", "192.168.1.1", "::1", "fd00::1"} {
		metadata := resolver.Enrich(requestmeta.New(address, "test-agent"))
		if metadata.IPCountryCode != "" || metadata.IPASN != 0 || metadata.IPASOrganization != "" {
			t.Fatalf("address %q unexpectedly enriched: %+v", address, metadata)
		}
	}
}

func TestPreferredGeoName(t *testing.T) {
	if got := preferredGeoName(map[string]string{"zh-CN": "新加坡", "en": "Singapore"}); got != "Singapore" {
		t.Fatalf("preferredGeoName() = %q", got)
	}
	if got := preferredGeoName(map[string]string{"zh": "新加坡"}); got != "新加坡" {
		t.Fatalf("preferredGeoName() fallback = %q", got)
	}
	if got := preferredGeoName(map[string]string{"fr": strings.Repeat("x", 20)}); got != "" {
		t.Fatalf("preferredGeoName() unknown language = %q", got)
	}
}
