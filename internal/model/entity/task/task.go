package task

import (
	"encoding/json"
	"time"
)

type Type int8

const (
	Undefined Type = -1
	TypeEmail      = iota
	TypeQuota
)

type Task struct {
	Id        int64     `gorm:"primaryKey;autoIncrement;comment:ID"`
	Type      int8      `gorm:"not null;comment:Task Type"`
	Scope     string    `gorm:"type:text;comment:Task Scope"`
	Content   string    `gorm:"type:text;comment:Task Content"`
	Status    int8      `gorm:"not null;default:0;comment:Task Status: 0: Pending, 1: In Progress, 2: Completed, 3: Failed"`
	Errors    string    `gorm:"type:text;comment:Task Errors"`
	Total     uint64    `gorm:"column:total;not null;default:0;comment:Total Number"`
	Current   uint64    `gorm:"column:current;not null;default:0;comment:Current Number"`
	CreatedAt time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (Task) TableName() string {
	return "task"
}

// Filter task 列表查询过滤条件
type Filter struct {
	Type   Type
	Page   int
	Size   int
	Status *uint8
	Scope  *int8
}

type ScopeType int8

const (
	ScopeAll     ScopeType = iota + 1 // All users
	ScopeActive                       // Active users
	ScopeExpired                      // Expired users
	ScopeNone                         // No Subscribe
	ScopeSkip                         // Skip user filtering
)

func (t ScopeType) Int8() int8 {
	return int8(t)
}

type EmailScope struct {
	Type              int8     `gorm:"not null;comment:Scope Type"`
	RegisterStartTime int64    `json:"register_start_time"`
	RegisterEndTime   int64    `json:"register_end_time"`
	Recipients        []string `json:"recipients"` // list of email addresses
	Additional        []string `json:"additional"` // additional email addresses
	Scheduled         int64    `json:"scheduled"`  // scheduled time (unix timestamp)
	Interval          uint8    `json:"interval"`   // interval in seconds
	Limit             uint64   `json:"limit"`      // daily send limit
}

func (s *EmailScope) Marshal() ([]byte, error) {
	type Alias EmailScope
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

func (s *EmailScope) Unmarshal(data []byte) error {
	type Alias EmailScope
	aux := (*Alias)(s)
	return json.Unmarshal(data, &aux)
}

type EmailContent struct {
	Subject string `json:"subject"`
	Content string `json:"content"`
}

func (c *EmailContent) Marshal() ([]byte, error) {
	type Alias EmailContent
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}

func (c *EmailContent) Unmarshal(data []byte) error {
	type Alias EmailContent
	aux := (*Alias)(c)
	return json.Unmarshal(data, &aux)
}

type QuotaScope struct {
	Subscribers []int64 `json:"subscribers"` // Subscribe IDs
	IsActive    *bool   `json:"is_active"`   // filter by active status
	StartTime   int64   `json:"start_time"`  // filter by subscription start time
	EndTime     int64   `json:"end_time"`    // filter by subscription end time
	Objects     []int64 `json:"recipients"`  // list of user subs IDs
}

func (s *QuotaScope) Marshal() ([]byte, error) {
	type Alias QuotaScope
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(s),
	})
}

func (s *QuotaScope) Unmarshal(data []byte) error {
	type Alias QuotaScope
	aux := (*Alias)(s)
	return json.Unmarshal(data, &aux)
}

type QuotaContent struct {
	ResetTraffic bool   `json:"reset_traffic"`        // whether to reset traffic
	Days         uint64 `json:"days,omitempty"`       // days to add
	GiftType     uint8  `json:"gift_type,omitempty"`  // 1: Fixed, 2: Ratio
	GiftValue    uint64 `json:"gift_value,omitempty"` // value of the gift type
}

func (c *QuotaContent) Marshal() ([]byte, error) {
	type Alias QuotaContent
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(c),
	})
}

func (c *QuotaContent) Unmarshal(data []byte) error {
	type Alias QuotaContent
	aux := (*Alias)(c)
	return json.Unmarshal(data, &aux)
}

func ParseScopeType(t int8) ScopeType {
	switch t {
	case 1:
		return ScopeAll
	case 2:
		return ScopeActive
	case 3:
		return ScopeExpired
	case 4:
		return ScopeNone
	default:
		return ScopeSkip
	}
}
