// Package log holds the system log row (the system_logs table) and the typed
// content of each kind of entry: the message and subscription logs, the
// login, registration, balance, commission, gift and order logs, and the
// traffic logs, rankings and daily statistics. Every domain may append to
// the audit log inside its own transaction.
package log

import (
	"context"
	"encoding/json"
	"time"

	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/timeutil"
)

type Type uint8

/*

Log Types:
	1X Message Logs
	2X Subscription Logs
	3X User Logs
	4X Traffic Ranking Logs
	5X Administration Logs
*/

const (
	TypeEmailMessage      Type = 10 // Message log
	TypeMobileMessage     Type = 11 // Mobile message log
	TypeSubscribe         Type = 20 // Subscription log
	TypeSubscribeTraffic  Type = 21 // Subscription traffic log
	TypeServerTraffic     Type = 22 // Server traffic log
	TypeResetSubscribe    Type = 23 // Reset subscription log
	TypeLogin             Type = 30 // Login log
	TypeRegister          Type = 31 // Registration log
	TypeBalance           Type = 32 // Balance log
	TypeCommission        Type = 33 // Commission log
	TypeGift              Type = 34 // Gift log
	TypeOrderCreated      Type = 35 // Order creation audit log
	TypeUnmatchedPayment  Type = 36 // Gateway-confirmed payment that could not settle an order
	TypeUserTrafficRank   Type = 40 // Top 10 User traffic rank log
	TypeServerTrafficRank Type = 41 // Top 10 Server traffic rank log
	TypeTrafficStat       Type = 42 // Daily traffic statistics log
	TypeAdminAction       Type = 50 // Administrator mutation audit log
)
const (
	ResetSubscribeTypeAuto       uint16 = 231 // Auto reset
	ResetSubscribeTypeAdvance    uint16 = 232 // Advance reset
	ResetSubscribeTypePaid       uint16 = 233 // Paid reset
	ResetSubscribeTypeQuota      uint16 = 234 // Quota reset
	BalanceTypeRecharge          uint16 = 321 // Recharge
	BalanceTypeWithdraw          uint16 = 322 // Withdraw
	BalanceTypePayment           uint16 = 323 // Payment
	BalanceTypeRefund            uint16 = 324 // Refund
	BalanceTypeAdjust            uint16 = 326 // Admin Adjust
	BalanceTypeReward            uint16 = 325 // Reward
	CommissionTypePurchase       uint16 = 331 // Purchase
	CommissionTypeRenewal        uint16 = 332 // Renewal
	CommissionTypeRefund         uint16 = 333 // Refund
	CommissionTypeWithdraw       uint16 = 334 // withdraw
	CommissionTypeAdjust         uint16 = 335 // Admin Adjust
	CommissionTypeConvertBalance uint16 = 336 // Convert to Balance
	GiftTypeIncrease             uint16 = 341 // Increase
	GiftTypeReduce               uint16 = 342 // Reduce
)

// Uint8 converts Type to uint8.
func (t Type) Uint8() uint8 {
	return uint8(t)
}

// ExpirableTypes returns the operational log classes that may be removed by
// the configured retention policy. Financial logs are deliberately absent:
// balance, commission and gift entries are durable ledgers and are also used
// by business queries, so treating them as disposable logs corrupts history.
// Keep this as an allowlist so newly-added log types are retained by default.
func ExpirableTypes() []int {
	return []int{
		int(TypeEmailMessage),
		int(TypeMobileMessage),
		int(TypeSubscribe),
		int(TypeSubscribeTraffic),
		int(TypeServerTraffic),
		int(TypeResetSubscribe),
		int(TypeLogin),
		int(TypeRegister),
		int(TypeUserTrafficRank),
		int(TypeServerTrafficRank),
		int(TypeTrafficStat),
	}
}

// FilterParams selects a page of the system log.
type FilterParams struct {
	Page      int
	Size      int
	Type      uint8
	Data      string
	StartDate string
	EndDate   string
	Search    string
	ObjectID  int64
	SkipCount bool // when true, skip the COUNT(*) query (total will be 0)
	// ContentInt64 keeps the rows whose JSON content has each named
	// top-level field equal to the value, matched by the database's JSON
	// extraction rather than by a text pattern (which "12" would share with
	// "120"). The keys are field names of the content types in this package.
	ContentInt64 map[string]int64
}

// SystemLog represents a log entry in the system.
type SystemLog struct {
	Id        int64     `gorm:"primaryKey;AUTO_INCREMENT"`
	Type      uint8     `gorm:"index:idx_type;type:tinyint(1);not null;default:0;comment:Log Type: 1: Email Message 2: Mobile Message 3: Subscribe 4: Subscribe Traffic 5: Server Traffic 6: Login 7: Register 8: Balance 9: Commission 10: Reset Subscribe 11: Gift"`
	Date      string    `gorm:"index:idx_date;type:varchar(20);default:null;comment:Log Date"`
	ObjectID  int64     `gorm:"index:idx_object_id;type:bigint(20);not null;default:0;comment:Object ID"`
	Content   string    `gorm:"type:text;not null;comment:Log Content"`
	CreatedAt time.Time `gorm:"<-:create;comment:Create Time"`
}

// TableName returns the name of the table for SystemLogs.
func (SystemLog) TableName() string {
	return "system_logs"
}

// The content types below are what system_logs.content stores as JSON. Their
// Marshal and Unmarshal methods — deliberately not json.Marshaler — encode
// and decode that stored form, and both run the type's clean method: the
// request that caused an entry is attacker-influenced, so it is bounded (and,
// where the type says so, redacted) when stored and again when read, which
// also covers rows stored before a bound existed. Every type goes through
// marshalEntry and unmarshalEntry, so none can skip the cleaning.

// cleaner is a content type that cleans itself in place.
type cleaner[T any] interface {
	*T
	clean()
}

// marshalEntry encodes a cleaned copy of entry.
func marshalEntry[T any, P cleaner[T]](entry *T) ([]byte, error) {
	safe := *entry
	P(&safe).clean()
	return json.Marshal(&safe)
}

// unmarshalEntry decodes stored content into entry and cleans it.
func unmarshalEntry[T any, P cleaner[T]](data []byte, entry *T) error {
	if err := json.Unmarshal(data, entry); err != nil {
		return err
	}
	P(entry).clean()
	return nil
}

// boundedRiskValue retains exact request metadata used by risk analysis while
// preventing attacker-controlled headers from growing an audit row without
// bound. The limits match the existing user_device storage contract.
func boundedRiskValue(value string, maxBytes int) string {
	return requestmeta.Bound(value, maxBytes)
}

func sanitizeRequestMetadata(metadata requestmeta.Metadata) requestmeta.Metadata {
	return requestmeta.Normalize(metadata)
}

func sanitizeIPMetadata(metadata requestmeta.IPMetadata) requestmeta.IPMetadata {
	return requestmeta.Normalize(requestmeta.Metadata{IPMetadata: metadata}).IPMetadata
}

// Message represents a message log entry.
type Message struct {
	requestmeta.Metadata
	To       string         `json:"to"`
	Subject  string         `json:"subject,omitempty"`
	Content  map[string]any `json:"content"`
	Platform string         `json:"platform"`
	Template string         `json:"template"`
	Status   uint8          `json:"status"` // 0: Attempt started, 1: Sent, 2: Failed
}

// Marshal encodes the entry as stored, redacted.
func (m *Message) Marshal() ([]byte, error) { return marshalEntry(m) }

// Unmarshal decodes a stored entry, redacted.
func (m *Message) Unmarshal(data []byte) error { return unmarshalEntry(data, m) }

func (m *Message) clean() { *m = sanitizeMessage(*m) }

func sanitizeMessage(message Message) Message {
	message.Metadata = sanitizeRequestMetadata(message.Metadata)
	message.To = logger.RedactedValue
	safeContent := map[string]any{"redacted": true}
	if emailType, ok := message.Content["email_type"].(string); ok && safeMessageCategory(emailType) {
		safeContent["email_type"] = emailType
	}
	switch taskID := message.Content["batch_task_id"].(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		safeContent["batch_task_id"] = taskID
	}
	message.Content = safeContent
	if message.Template != "" {
		message.Template = logger.RedactedValue
	}
	// Retain only fixed operational categories. Historical custom subjects can
	// contain names, addresses or one-time credentials and must not be exposed.
	if !safeMessageCategory(message.Subject) {
		message.Subject = logger.RedactedValue
	}
	return message
}

func safeMessageCategory(value string) bool {
	switch value {
	case "verify", "maintenance", "expiration", "traffic_exceed", "custom", "register", "security", "unknown":
		return true
	default:
		return false
	}
}

// Traffic represents a subscription traffic log entry.
type Traffic struct {
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}

// Marshal encodes the entry as stored.
func (s *Traffic) Marshal() ([]byte, error) { return marshalEntry(s) }

// Unmarshal decodes a stored entry.
func (s *Traffic) Unmarshal(data []byte) error { return unmarshalEntry(data, s) }

// clean has nothing to bound: the entry records no request.
func (s *Traffic) clean() {}

// Login represents a login log entry.
type Login struct {
	requestmeta.IPMetadata
	Method    string `json:"method"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
	ActorID   int64  `json:"actor_id,omitempty"`
}

// Marshal encodes the entry as stored, its request bounded.
func (l *Login) Marshal() ([]byte, error) { return marshalEntry(l) }

// Unmarshal decodes a stored entry, its request bounded.
func (l *Login) Unmarshal(data []byte) error { return unmarshalEntry(data, l) }

// Request returns the request the login came from; its address is LoginIP.
func (l *Login) Request() requestmeta.Metadata {
	return requestmeta.Metadata{ClientIP: l.LoginIP, UserAgent: l.UserAgent, ActorID: l.ActorID, IPMetadata: l.IPMetadata}
}

func (l *Login) clean() {
	l.LoginIP = boundedRiskValue(l.LoginIP, requestmeta.MaxClientIPBytes)
	l.UserAgent = boundedRiskValue(l.UserAgent, requestmeta.MaxUserAgentBytes)
	l.IPMetadata = sanitizeIPMetadata(l.IPMetadata)
}

// Register represents a registration log entry.
type Register struct {
	requestmeta.IPMetadata
	AuthMethod string `json:"auth_method"`
	Identifier string `json:"identifier"`
	RegisterIP string `json:"register_ip"`
	UserAgent  string `json:"user_agent"`
	Timestamp  int64  `json:"timestamp"`
	ActorID    int64  `json:"actor_id,omitempty"`
}

// Marshal encodes the entry as stored: the identifier redacted, the request
// bounded.
func (r *Register) Marshal() ([]byte, error) { return marshalEntry(r) }

// Unmarshal decodes a stored entry: the identifier redacted, the request
// bounded.
func (r *Register) Unmarshal(data []byte) error { return unmarshalEntry(data, r) }

// Request returns the request the registration came from; its address is
// RegisterIP.
func (r *Register) Request() requestmeta.Metadata {
	return requestmeta.Metadata{ClientIP: r.RegisterIP, UserAgent: r.UserAgent, ActorID: r.ActorID, IPMetadata: r.IPMetadata}
}

func (r *Register) clean() {
	r.Identifier = logger.RedactedValue
	r.RegisterIP = boundedRiskValue(r.RegisterIP, requestmeta.MaxClientIPBytes)
	r.UserAgent = boundedRiskValue(r.UserAgent, requestmeta.MaxUserAgentBytes)
	r.IPMetadata = sanitizeIPMetadata(r.IPMetadata)
}

// Subscribe represents a subscription log entry.
type Subscribe struct {
	requestmeta.IPMetadata
	Token           string `json:"token"`
	UserAgent       string `json:"user_agent"`
	ClientIP        string `json:"client_ip"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
	ActorID         int64  `json:"actor_id,omitempty"`
}

// Marshal encodes the entry as stored: the token redacted, the request
// bounded.
func (s *Subscribe) Marshal() ([]byte, error) { return marshalEntry(s) }

// Unmarshal decodes a stored entry: the token redacted, the request bounded.
func (s *Subscribe) Unmarshal(data []byte) error { return unmarshalEntry(data, s) }

// Request returns the request that fetched the subscription.
func (s *Subscribe) Request() requestmeta.Metadata {
	return requestmeta.Metadata{ClientIP: s.ClientIP, UserAgent: s.UserAgent, ActorID: s.ActorID, IPMetadata: s.IPMetadata}
}

func (s *Subscribe) clean() {
	s.Token = logger.RedactedValue
	s.UserAgent = boundedRiskValue(s.UserAgent, requestmeta.MaxUserAgentBytes)
	s.ClientIP = boundedRiskValue(s.ClientIP, requestmeta.MaxClientIPBytes)
	s.IPMetadata = sanitizeIPMetadata(s.IPMetadata)
}

// ResetSubscribe represents a reset subscription log entry.
type ResetSubscribe struct {
	requestmeta.Metadata
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	OrderNo   string `json:"order_no,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (r *ResetSubscribe) Marshal() ([]byte, error) { return marshalEntry(r) }

// Unmarshal decodes a stored entry, its request bounded.
func (r *ResetSubscribe) Unmarshal(data []byte) error { return unmarshalEntry(data, r) }

func (r *ResetSubscribe) clean() { r.Metadata = sanitizeRequestMetadata(r.Metadata) }

// Balance represents a balance log entry.
type Balance struct {
	requestmeta.Metadata
	Type      uint16 `json:"type"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no,omitempty"`
	Balance   int64  `json:"balance"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (b *Balance) Marshal() ([]byte, error) { return marshalEntry(b) }

// Unmarshal decodes a stored entry, its request bounded.
func (b *Balance) Unmarshal(data []byte) error { return unmarshalEntry(data, b) }

func (b *Balance) clean() { b.Metadata = sanitizeRequestMetadata(b.Metadata) }

// Commission represents a commission log entry.
type Commission struct {
	requestmeta.Metadata
	Type    uint16 `json:"type"`
	Amount  int64  `json:"amount"`
	OrderNo string `json:"order_no"`
	// Balance is the commission balance after the movement, so the balance
	// before it is Balance - Amount. Writers that do not know it leave it
	// out; an administrator's adjustment always records it.
	Balance   int64 `json:"balance,omitempty"`
	Timestamp int64 `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (c *Commission) Marshal() ([]byte, error) { return marshalEntry(c) }

// Unmarshal decodes a stored entry, its request bounded.
func (c *Commission) Unmarshal(data []byte) error { return unmarshalEntry(data, c) }

func (c *Commission) clean() { c.Metadata = sanitizeRequestMetadata(c.Metadata) }

// Gift represents a gift log entry.
type Gift struct {
	requestmeta.Metadata
	Type        uint16 `json:"type"`
	OrderNo     string `json:"order_no"`
	SubscribeId int64  `json:"subscribe_id"`
	Amount      int64  `json:"amount"`
	Balance     int64  `json:"balance"`
	Remark      string `json:"remark,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (g *Gift) Marshal() ([]byte, error) { return marshalEntry(g) }

// Unmarshal decodes a stored entry, its request bounded.
func (g *Gift) Unmarshal(data []byte) error { return unmarshalEntry(data, g) }

func (g *Gift) clean() { g.Metadata = sanitizeRequestMetadata(g.Metadata) }

// OrderCreated represents a durable order-creation audit entry. It contains
// only the order summary needed for operations and risk analysis; coupon
// codes, gateway trade numbers and guest credentials are deliberately absent.
type OrderCreated struct {
	requestmeta.Metadata
	OrderNo        string `json:"order_no"`
	OrderType      uint8  `json:"order_type"`
	Quantity       int64  `json:"quantity"`
	Price          int64  `json:"price"`
	Amount         int64  `json:"amount"`
	GiftAmount     int64  `json:"gift_amount"`
	Discount       int64  `json:"discount"`
	CouponDiscount int64  `json:"coupon_discount"`
	PaymentID      int64  `json:"payment_id"`
	Method         string `json:"method"`
	FeeAmount      int64  `json:"fee_amount"`
	SubscribeID    int64  `json:"subscribe_id,omitempty"`
	Source         string `json:"source"`
	Timestamp      int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (o *OrderCreated) Marshal() ([]byte, error) { return marshalEntry(o) }

// Unmarshal decodes a stored entry, its request bounded.
func (o *OrderCreated) Unmarshal(data []byte) error { return unmarshalEntry(data, o) }

func (o *OrderCreated) clean() { o.Metadata = sanitizeRequestMetadata(o.Metadata) }

// UserTraffic represents a user traffic log entry.
type UserTraffic struct {
	requestmeta.Metadata
	SubscribeId int64 `json:"subscribe_id"` // Subscribe ID
	UserId      int64 `json:"user_id"`      // User ID
	Upload      int64 `json:"upload"`       // Upload traffic in bytes
	Download    int64 `json:"download"`     // Download traffic in bytes
	Total       int64 `json:"total"`        // Total traffic in bytes (Upload + Download)
}

// Marshal encodes the entry as stored, its request bounded.
func (u *UserTraffic) Marshal() ([]byte, error) { return marshalEntry(u) }

// Unmarshal decodes a stored entry, its request bounded.
func (u *UserTraffic) Unmarshal(data []byte) error { return unmarshalEntry(data, u) }

func (u *UserTraffic) clean() { u.Metadata = sanitizeRequestMetadata(u.Metadata) }

// UserTrafficRank represents a user traffic rank entry.
type UserTrafficRank struct {
	Rank map[uint8]UserTraffic `json:"rank"` // Key is rank ,type is UserTraffic
}

// Marshal encodes the ranking as stored, each entry's request bounded.
func (u *UserTrafficRank) Marshal() ([]byte, error) { return marshalEntry(u) }

// Unmarshal decodes a stored ranking, each entry's request bounded.
func (u *UserTrafficRank) Unmarshal(data []byte) error { return unmarshalEntry(data, u) }

// clean bounds every entry. It replaces the map, so the copy marshalEntry
// cleans never writes through to the caller's ranking.
func (u *UserTrafficRank) clean() {
	if u.Rank == nil {
		return
	}
	rank := make(map[uint8]UserTraffic, len(u.Rank))
	for position, entry := range u.Rank {
		entry.clean()
		rank[position] = entry
	}
	u.Rank = rank
}

// ServerTraffic represents a server traffic log entry.
type ServerTraffic struct {
	requestmeta.Metadata
	ServerId int64 `json:"server_id"` // Server ID
	Upload   int64 `json:"upload"`    // Upload traffic in bytes
	Download int64 `json:"download"`  // Download traffic in bytes
	Total    int64 `json:"total"`     // Total traffic in bytes (Upload + Download)
}

// Marshal encodes the entry as stored, its request bounded.
func (s *ServerTraffic) Marshal() ([]byte, error) { return marshalEntry(s) }

// Unmarshal decodes a stored entry, its request bounded.
func (s *ServerTraffic) Unmarshal(data []byte) error { return unmarshalEntry(data, s) }

func (s *ServerTraffic) clean() { s.Metadata = sanitizeRequestMetadata(s.Metadata) }

// ServerTrafficRank represents a server traffic rank entry.
type ServerTrafficRank struct {
	Rank map[uint8]ServerTraffic `json:"rank"` // Key is rank ,type is ServerTraffic
}

// Marshal encodes the ranking as stored, each entry's request bounded.
func (s *ServerTrafficRank) Marshal() ([]byte, error) { return marshalEntry(s) }

// Unmarshal decodes a stored ranking, each entry's request bounded.
func (s *ServerTrafficRank) Unmarshal(data []byte) error { return unmarshalEntry(data, s) }

// clean bounds every entry; see UserTrafficRank.clean.
func (s *ServerTrafficRank) clean() {
	if s.Rank == nil {
		return
	}
	rank := make(map[uint8]ServerTraffic, len(s.Rank))
	for position, entry := range s.Rank {
		entry.clean()
		rank[position] = entry
	}
	s.Rank = rank
}

// TrafficStat represents a daily traffic statistics log entry.
type TrafficStat struct {
	requestmeta.Metadata
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
}

// Marshal encodes the entry as stored, its request bounded.
func (t *TrafficStat) Marshal() ([]byte, error) { return marshalEntry(t) }

// Unmarshal decodes a stored entry, its request bounded.
func (t *TrafficStat) Unmarshal(data []byte) error { return unmarshalEntry(data, t) }

func (t *TrafficStat) clean() { t.Metadata = sanitizeRequestMetadata(t.Metadata) }

// UnmatchedPayment records a payment a gateway confirmed that could not
// settle its order: the order was already closed or finished, the trade
// differs from the one bound to it, or the gateway asks for manual review. It
// is the durable trace an operator refunds from, so it is a financial record
// and never expires.
type UnmatchedPayment struct {
	requestmeta.Metadata
	OrderNo   string `json:"order_no"`
	TradeNo   string `json:"trade_no"`
	Platform  string `json:"platform"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Reason    string `json:"reason"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (u *UnmatchedPayment) Marshal() ([]byte, error) { return marshalEntry(u) }

// Unmarshal decodes a stored entry, its request bounded.
func (u *UnmatchedPayment) Unmarshal(data []byte) error { return unmarshalEntry(data, u) }

func (u *UnmatchedPayment) clean() {
	u.Metadata = sanitizeRequestMetadata(u.Metadata)
	u.Reason = requestmeta.Bound(u.Reason, maxAdminDetailBytes)
}

// maxAdminDetailBytes bounds the free-text detail of an administration entry.
const maxAdminDetailBytes = 2048

// The sources an administrator mutation arrives from.
const (
	AdminActionSourceHTTP     = "http"
	AdminActionSourceTelegram = "telegram"
)

// NewAdminActionLog builds the system log row recording action, dated now.
// The row's object is the acting administrator (action.ActorID), so the
// trail of one administrator is an indexed read; the object the action
// changed is in the content. An unset Source is the HTTP API and an unset
// Timestamp is now.
func NewAdminActionLog(action AdminAction) (*SystemLog, error) {
	now := timeutil.Now()
	if action.Timestamp == 0 {
		action.Timestamp = now.UnixMilli()
	}
	if action.Source == "" {
		action.Source = AdminActionSourceHTTP
	}
	content, err := action.Marshal()
	if err != nil {
		return nil, err
	}
	return &SystemLog{
		Type:     TypeAdminAction.Uint8(),
		Date:     now.Format(time.DateOnly),
		ObjectID: action.ActorID,
		Content:  string(content),
	}, nil
}

// AdminActionFrom returns action with the request metadata of ctx (the
// administrator's request, ActorID included) as recorded by the HTTP
// middleware; without one the action is recorded as it is.
func AdminActionFrom(ctx context.Context, action AdminAction) AdminAction {
	if metadata, ok := requestmeta.From(ctx); ok {
		actor := action.ActorID
		action.Metadata = metadata
		if actor != 0 {
			action.ActorID = actor
		}
	}
	return action
}

// AdminAction records a mutation an administrator made: which action, on
// which object, from the HTTP API (ActorID) or the Telegram bot
// (TelegramSenderID). Detail is a short, bounded description and must never
// carry a secret: settings entries name the changed keys, not their values.
// The type is not expirable, so the trail survives a shortened retention.
type AdminAction struct {
	requestmeta.Metadata
	Action           string `json:"action"`
	Object           string `json:"object,omitempty"`
	ObjectID         int64  `json:"object_id,omitempty"`
	Detail           string `json:"detail,omitempty"`
	Source           string `json:"source"` // "http" or "telegram"
	TelegramSenderID int64  `json:"telegram_sender_id,omitempty"`
	Timestamp        int64  `json:"timestamp"`
}

// Marshal encodes the entry as stored, its request bounded.
func (a *AdminAction) Marshal() ([]byte, error) { return marshalEntry(a) }

// Unmarshal decodes a stored entry, its request bounded.
func (a *AdminAction) Unmarshal(data []byte) error { return unmarshalEntry(data, a) }

func (a *AdminAction) clean() {
	a.Metadata = sanitizeRequestMetadata(a.Metadata)
	a.Detail = requestmeta.Bound(a.Detail, maxAdminDetailBytes)
}
