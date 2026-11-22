package log

import (
	"strings"
	"testing"

	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// storedEncodings are the system_logs.content encodings of goldenEntries,
// taken from the implementation before its clean-up. Stored rows are read
// back years later, so the encoding must never change.
var storedEncodings = map[string]string{
	"message":             "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"to\":\"[REDACTED]\",\"subject\":\"verify\",\"content\":{\"email_type\":\"verify\",\"redacted\":true},\"platform\":\"smtp\",\"template\":\"[REDACTED]\",\"status\":1}",
	"traffic":             "{\"download\":10,\"upload\":20}",
	"login":               "{\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"method\":\"email\",\"login_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"success\":true,\"timestamp\":1700000000000,\"actor_id\":9}",
	"register":            "{\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"auth_method\":\"email\",\"identifier\":\"[REDACTED]\",\"register_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"timestamp\":1700000000000,\"actor_id\":9}",
	"subscribe":           "{\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"token\":\"[REDACTED]\",\"user_agent\":\"Clash/1.0\",\"client_ip\":\"203.0.113.7\",\"user_subscribe_id\":5,\"actor_id\":9}",
	"reset_subscribe":     "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"type\":233,\"user_id\":7,\"order_no\":\"N1\",\"timestamp\":1700000000000}",
	"balance":             "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"type\":321,\"amount\":100,\"order_no\":\"N1\",\"balance\":500,\"timestamp\":1700000000000}",
	"commission":          "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"type\":331,\"amount\":10,\"order_no\":\"N1\",\"timestamp\":1700000000000}",
	"gift":                "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"type\":341,\"order_no\":\"N1\",\"subscribe_id\":3,\"amount\":1,\"balance\":2,\"remark\":\"promo\",\"timestamp\":1700000000000}",
	"order_created":       "{\"client_ip\":\"203.0.113.7\",\"user_agent\":\"RiskClient/1.0\",\"actor_id\":9,\"ip_country_code\":\"SG\",\"ip_country\":\"Singapore\",\"ip_region\":\"Central\",\"ip_city\":\"Singapore\",\"ip_asn\":13335,\"ip_as_organization\":\"Cloudflare\",\"order_no\":\"N1\",\"order_type\":1,\"quantity\":1,\"price\":100,\"amount\":90,\"gift_amount\":5,\"discount\":5,\"coupon_discount\":0,\"payment_id\":2,\"method\":\"balance\",\"fee_amount\":0,\"subscribe_id\":3,\"source\":\"web\",\"timestamp\":1700000000000}",
	"user_traffic":        "{\"subscribe_id\":5,\"user_id\":7,\"upload\":1,\"download\":2,\"total\":3}",
	"user_traffic_rank":   "{\"rank\":{\"1\":{\"subscribe_id\":5,\"user_id\":7,\"upload\":1,\"download\":2,\"total\":3}}}",
	"server_traffic":      "{\"server_id\":4,\"upload\":1,\"download\":2,\"total\":3}",
	"server_traffic_rank": "{\"rank\":{\"1\":{\"server_id\":4,\"upload\":1,\"download\":2,\"total\":3}}}",
	"traffic_stat":        "{\"upload\":1,\"download\":2,\"total\":3}",
}

func TestLogContentEncodingIsStable(t *testing.T) {
	for _, entry := range goldenEntries() {
		data, err := entry.value.Marshal()
		if err != nil {
			t.Fatalf("%s: Marshal: %v", entry.name, err)
		}
		if want := storedEncodings[entry.name]; string(data) != want {
			t.Errorf("%s encodes as\n%s\nwant\n%s", entry.name, data, want)
		}
		// A stored row decodes and re-encodes to itself.
		if err := entry.value.Unmarshal(data); err != nil {
			t.Fatalf("%s: Unmarshal: %v", entry.name, err)
		}
		again, err := entry.value.Marshal()
		if err != nil || string(again) != string(data) {
			t.Errorf("%s round trip = %s (%v), want %s", entry.name, again, err, data)
		}
	}
}

type storedEntry interface {
	Marshal() ([]byte, error)
	Unmarshal([]byte) error
}

// Every entry that records a request bounds it, on the way in and on the way
// out — the traffic logs included, which used to skip it.
func TestLogContentBoundsRequestMetadata(t *testing.T) {
	long := requestmeta.Metadata{
		ClientIP:   strings.Repeat("1", requestmeta.MaxClientIPBytes+10),
		UserAgent:  strings.Repeat("a", requestmeta.MaxUserAgentBytes+10),
		IPMetadata: requestmeta.IPMetadata{IPCity: strings.Repeat("c", requestmeta.MaxCityBytes+10)},
	}
	raw := []byte(`{"client_ip":"` + long.ClientIP + `","user_agent":"` + long.UserAgent + `","ip_city":"` + long.IPCity + `","upload":1}`)

	balance := &Balance{Metadata: long}
	userTraffic := &UserTraffic{Metadata: long}
	serverTraffic := &ServerTraffic{Metadata: long}
	stat := &TrafficStat{Metadata: long}
	for name, tt := range map[string]struct {
		entry storedEntry
		read  func() requestmeta.Metadata
	}{
		"balance":        {balance, func() requestmeta.Metadata { return balance.Metadata }},
		"user_traffic":   {userTraffic, func() requestmeta.Metadata { return userTraffic.Metadata }},
		"server_traffic": {serverTraffic, func() requestmeta.Metadata { return serverTraffic.Metadata }},
		"traffic_stat":   {stat, func() requestmeta.Metadata { return stat.Metadata }},
	} {
		data, err := tt.entry.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), long.UserAgent) {
			t.Fatalf("%s: the stored entry carries the unbounded user agent", name)
		}
		// Rows stored before the bound are bounded when read.
		if err := tt.entry.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		got := tt.read()
		if len(got.ClientIP) > requestmeta.MaxClientIPBytes || len(got.UserAgent) > requestmeta.MaxUserAgentBytes || len(got.IPCity) > requestmeta.MaxCityBytes {
			t.Fatalf("%s: read metadata not bounded: %d/%d/%d bytes", name, len(got.ClientIP), len(got.UserAgent), len(got.IPCity))
		}
	}
	if stat.Upload != 1 {
		t.Fatalf("upload = %d, want the rest of the row decoded", stat.Upload)
	}

	for name, rank := range map[string]storedEntry{
		"user_rank":   &UserTrafficRank{Rank: map[uint8]UserTraffic{1: {Metadata: long}}},
		"server_rank": &ServerTrafficRank{Rank: map[uint8]ServerTraffic{1: {Metadata: long}}},
	} {
		data, err := rank.Marshal()
		if err != nil || strings.Contains(string(data), long.UserAgent) {
			t.Fatalf("%s entries not bounded: %s (%v)", name, data, err)
		}
	}
}
