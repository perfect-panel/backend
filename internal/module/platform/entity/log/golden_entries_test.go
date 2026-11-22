package log

import "github.com/perfect-panel/backend/pkg/requestmeta"

type goldenEntry struct {
	name  string
	value interface {
		Marshal() ([]byte, error)
		Unmarshal([]byte) error
	}
}

var goldenRequest = requestmeta.Metadata{
	ClientIP:  "203.0.113.7",
	UserAgent: "RiskClient/1.0",
	ActorID:   9,
	IPMetadata: requestmeta.IPMetadata{
		IPCountryCode: "SG", IPCountry: "Singapore", IPRegion: "Central", IPCity: "Singapore",
		IPASN: 13335, IPASOrganization: "Cloudflare",
	},
}

// goldenEntries is one populated entry of every log content type.
func goldenEntries() []goldenEntry {
	return []goldenEntry{
		{"message", &Message{Metadata: goldenRequest, To: "user@example.com", Subject: "verify", Content: map[string]any{"email_type": "verify", "code": "123456"}, Platform: "smtp", Template: "{{.Code}}", Status: 1}},
		{"traffic", &Traffic{Download: 10, Upload: 20}},
		{"login", &Login{IPMetadata: goldenRequest.IPMetadata, Method: "email", LoginIP: "203.0.113.7", UserAgent: "RiskClient/1.0", Success: true, Timestamp: 1700000000000, ActorID: 9}},
		{"register", &Register{IPMetadata: goldenRequest.IPMetadata, AuthMethod: "email", Identifier: "user@example.com", RegisterIP: "203.0.113.7", UserAgent: "RiskClient/1.0", Timestamp: 1700000000000, ActorID: 9}},
		{"subscribe", &Subscribe{IPMetadata: goldenRequest.IPMetadata, Token: "secret-token", UserAgent: "Clash/1.0", ClientIP: "203.0.113.7", UserSubscribeId: 5, ActorID: 9}},
		{"reset_subscribe", &ResetSubscribe{Metadata: goldenRequest, Type: ResetSubscribeTypePaid, UserId: 7, OrderNo: "N1", Timestamp: 1700000000000}},
		{"balance", &Balance{Metadata: goldenRequest, Type: BalanceTypeRecharge, Amount: 100, OrderNo: "N1", Balance: 500, Timestamp: 1700000000000}},
		{"commission", &Commission{Metadata: goldenRequest, Type: CommissionTypePurchase, Amount: 10, OrderNo: "N1", Timestamp: 1700000000000}},
		{"gift", &Gift{Metadata: goldenRequest, Type: GiftTypeIncrease, OrderNo: "N1", SubscribeId: 3, Amount: 1, Balance: 2, Remark: "promo", Timestamp: 1700000000000}},
		{"order_created", &OrderCreated{Metadata: goldenRequest, OrderNo: "N1", OrderType: 1, Quantity: 1, Price: 100, Amount: 90, GiftAmount: 5, Discount: 5, CouponDiscount: 0, PaymentID: 2, Method: "balance", FeeAmount: 0, SubscribeID: 3, Source: "web", Timestamp: 1700000000000}},
		{"user_traffic", &UserTraffic{SubscribeId: 5, UserId: 7, Upload: 1, Download: 2, Total: 3}},
		{"user_traffic_rank", &UserTrafficRank{Rank: map[uint8]UserTraffic{1: {SubscribeId: 5, UserId: 7, Upload: 1, Download: 2, Total: 3}}}},
		{"server_traffic", &ServerTraffic{ServerId: 4, Upload: 1, Download: 2, Total: 3}},
		{"server_traffic_rank", &ServerTrafficRank{Rank: map[uint8]ServerTraffic{1: {ServerId: 4, Upload: 1, Download: 2, Total: 3}}}},
		{"traffic_stat", &TrafficStat{Upload: 1, Download: 2, Total: 3}},
	}
}
