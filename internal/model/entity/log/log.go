package log

import (
	"encoding/json"
	"time"
)

type Type uint8

/*

Log Types:
	1X Message Logs
	2X Subscription Logs
	3X User Logs
	4X Traffic Ranking Logs
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
	TypeUserTrafficRank   Type = 40 // Top 10 User traffic rank log
	TypeServerTrafficRank Type = 41 // Top 10 Server traffic rank log
	TypeTrafficStat       Type = 42 // Daily traffic statistics log
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

// FilterParams log 列表查询过滤条件
type FilterParams struct {
	Page      int
	Size      int
	Type      uint8
	Data      string
	Search    string
	ObjectID  int64
	SkipCount bool // when true, skip the COUNT(*) query (total will be 0)
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

// Message represents a message log entry.
type Message struct {
	To       string                 `json:"to"`
	Subject  string                 `json:"subject,omitempty"`
	Content  map[string]interface{} `json:"content"`
	Platform string                 `json:"platform"`
	Template string                 `json:"template"`
	Status   uint8                  `json:"status"` // 1: Sent, 2: Failed
}

// Marshal implements the json.Marshaler interface for Message.
func (m *Message) Marshal() ([]byte, error) {
	type Alias Message
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(m),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Message.
func (m *Message) Unmarshal(data []byte) error {
	type Alias Message
	aux := (*Alias)(m)
	return json.Unmarshal(data, aux)
}

// Traffic represents a subscription traffic log entry.
type Traffic struct {
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}

// Marshal implements the json.Marshaler interface for SubscribeTraffic.
func (s *Traffic) Marshal() ([]byte, error) {
	type Alias Traffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for SubscribeTraffic.
func (s *Traffic) Unmarshal(data []byte) error {
	type Alias Traffic
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// Login represents a login log entry.
type Login struct {
	Method    string `json:"method"`
	LoginIP   string `json:"login_ip"`
	UserAgent string `json:"user_agent"`
	Success   bool   `json:"success"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Login.
func (l *Login) Marshal() ([]byte, error) {
	type Alias Login
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(l),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Login.
func (l *Login) Unmarshal(data []byte) error {
	type Alias Login
	aux := (*Alias)(l)
	return json.Unmarshal(data, aux)
}

// Register represents a registration log entry.
type Register struct {
	AuthMethod string `json:"auth_method"`
	Identifier string `json:"identifier"`
	RegisterIP string `json:"register_ip"`
	UserAgent  string `json:"user_agent"`
	Timestamp  int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Register.
func (r *Register) Marshal() ([]byte, error) {
	type Alias Register
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Register.

func (r *Register) Unmarshal(data []byte) error {
	type Alias Register
	aux := (*Alias)(r)
	return json.Unmarshal(data, aux)
}

// Subscribe represents a subscription log entry.
type Subscribe struct {
	Token           string `json:"token"`
	UserAgent       string `json:"user_agent"`
	ClientIP        string `json:"client_ip"`
	UserSubscribeId int64  `json:"user_subscribe_id"`
}

// Marshal implements the json.Marshaler interface for Subscribe.
func (s *Subscribe) Marshal() ([]byte, error) {
	type Alias Subscribe
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Subscribe.
func (s *Subscribe) Unmarshal(data []byte) error {
	type Alias Subscribe
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// ResetSubscribe represents a reset subscription log entry.
type ResetSubscribe struct {
	Type      uint16 `json:"type"`
	UserId    int64  `json:"user_id"`
	OrderNo   string `json:"order_no,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for ResetSubscribe.
func (r *ResetSubscribe) Marshal() ([]byte, error) {
	type Alias ResetSubscribe
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(r),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ResetSubscribe.
func (r *ResetSubscribe) Unmarshal(data []byte) error {
	type Alias ResetSubscribe
	aux := (*Alias)(r)
	return json.Unmarshal(data, aux)
}

// Balance represents a balance log entry.
type Balance struct {
	Type      uint16 `json:"type"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no,omitempty"`
	Balance   int64  `json:"balance"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Balance.
func (b *Balance) Marshal() ([]byte, error) {
	type Alias Balance
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(b),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Balance.
func (b *Balance) Unmarshal(data []byte) error {
	type Alias Balance
	aux := (*Alias)(b)
	return json.Unmarshal(data, aux)
}

// Commission represents a commission log entry.
type Commission struct {
	Type      uint16 `json:"type"`
	Amount    int64  `json:"amount"`
	OrderNo   string `json:"order_no"`
	Timestamp int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Commission.
func (c *Commission) Marshal() ([]byte, error) {
	type Alias Commission
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Commission.
func (c *Commission) Unmarshal(data []byte) error {
	type Alias Commission
	aux := (*Alias)(c)
	return json.Unmarshal(data, aux)
}

// Gift represents a gift log entry.
type Gift struct {
	Type        uint16 `json:"type"`
	OrderNo     string `json:"order_no"`
	SubscribeId int64  `json:"subscribe_id"`
	Amount      int64  `json:"amount"`
	Balance     int64  `json:"balance"`
	Remark      string `json:"remark,omitempty"`
	Timestamp   int64  `json:"timestamp"`
}

// Marshal implements the json.Marshaler interface for Gift.
func (g *Gift) Marshal() ([]byte, error) {
	type Alias Gift
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(g),
	})
}

// Unmarshal implements the json.Unmarshaler interface for Gift.
func (g *Gift) Unmarshal(data []byte) error {
	type Alias Gift
	aux := (*Alias)(g)
	return json.Unmarshal(data, aux)
}

// UserTraffic represents a user traffic log entry.
type UserTraffic struct {
	SubscribeId int64 `json:"subscribe_id"` // Subscribe ID
	UserId      int64 `json:"user_id"`      // User ID
	Upload      int64 `json:"upload"`       // Upload traffic in bytes
	Download    int64 `json:"download"`     // Download traffic in bytes
	Total       int64 `json:"total"`        // Total traffic in bytes (Upload + Download)
}

// Marshal implements the json.Marshaler interface for UserTraffic.
func (u *UserTraffic) Marshal() ([]byte, error) {
	type Alias UserTraffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(u),
	})
}

// Unmarshal implements the json.Unmarshaler interface for UserTraffic.
func (u *UserTraffic) Unmarshal(data []byte) error {
	type Alias UserTraffic
	aux := (*Alias)(u)
	return json.Unmarshal(data, aux)
}

// UserTrafficRank represents a user traffic rank entry.
type UserTrafficRank struct {
	Rank map[uint8]UserTraffic `json:"rank"` // Key is rank ,type is UserTraffic
}

// Marshal implements the json.Marshaler interface for UserTrafficRank.
func (u *UserTrafficRank) Marshal() ([]byte, error) {
	type Alias UserTrafficRank
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(u),
	})
}

// Unmarshal implements the json.Unmarshaler interface for UserTrafficRank.
func (u *UserTrafficRank) Unmarshal(data []byte) error {
	type Alias UserTrafficRank
	aux := (*Alias)(u)
	return json.Unmarshal(data, aux)
}

// ServerTraffic represents a server traffic log entry.
type ServerTraffic struct {
	ServerId int64 `json:"server_id"` // Server ID
	Upload   int64 `json:"upload"`    // Upload traffic in bytes
	Download int64 `json:"download"`  // Download traffic in bytes
	Total    int64 `json:"total"`     // Total traffic in bytes (Upload + Download)
}

// Marshal implements the json.Marshaler interface for ServerTraffic.
func (s *ServerTraffic) Marshal() ([]byte, error) {
	type Alias ServerTraffic
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ServerTraffic.
func (s *ServerTraffic) Unmarshal(data []byte) error {
	type Alias ServerTraffic
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// ServerTrafficRank represents a server traffic rank entry.
type ServerTrafficRank struct {
	Rank map[uint8]ServerTraffic `json:"rank"` // Key is rank ,type is ServerTraffic
}

// Marshal implements the json.Marshaler interface for ServerTrafficRank.
func (s *ServerTrafficRank) Marshal() ([]byte, error) {
	type Alias ServerTrafficRank
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

// Unmarshal implements the json.Unmarshaler interface for ServerTrafficRank.
func (s *ServerTrafficRank) Unmarshal(data []byte) error {
	type Alias ServerTrafficRank
	aux := (*Alias)(s)
	return json.Unmarshal(data, aux)
}

// TrafficStat represents a daily traffic statistics log entry.
type TrafficStat struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
}

// Marshal implements the json.Marshaler interface for TrafficStat.
func (t *TrafficStat) Marshal() ([]byte, error) {
	type Alias TrafficStat
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(t),
	})
}

// Unmarshal implements the json.Unmarshaler interface for TrafficStat.
func (t *TrafficStat) Unmarshal(data []byte) error {
	type Alias TrafficStat
	aux := (*Alias)(t)
	return json.Unmarshal(data, aux)
}
