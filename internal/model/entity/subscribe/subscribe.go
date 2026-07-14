package subscribe

import (
	"time"

	"gorm.io/gorm"
)

type Subscribe struct {
	Id                int64     `gorm:"primaryKey"`
	Name              string    `gorm:"type:varchar(255);not null;default:'';comment:Subscribe Name"`
	Language          string    `gorm:"type:varchar(255);not null;default:'';comment:Language"`
	Description       string    `gorm:"type:text;comment:Subscribe Description"`
	UnitPrice         int64     `gorm:"type:int;not null;default:0;comment:Unit Price"`
	UnitTime          string    `gorm:"type:varchar(255);not null;default:'';comment:Unit Time"`
	Discount          string    `gorm:"type:text;comment:Discount"`
	Replacement       int64     `gorm:"type:int;not null;default:0;comment:Replacement"`
	Inventory         int64     `gorm:"type:int;not null;default:-1;comment:Inventory"`
	Traffic           int64     `gorm:"type:int;not null;default:0;comment:Traffic"`
	SpeedLimit        int64     `gorm:"type:int;not null;default:0;comment:Speed Limit"`
	DeviceLimit       int64     `gorm:"type:int;not null;default:0;comment:Device Limit"`
	Quota             int64     `gorm:"type:int;not null;default:0;comment:Quota"`
	Nodes             string    `gorm:"type:varchar(255);comment:Node Ids"`
	NodeTags          string    `gorm:"type:varchar(255);comment:Node Tags"`
	Show              *bool     `gorm:"type:tinyint(1);not null;default:0;comment:Show portal page"`
	Sell              *bool     `gorm:"type:tinyint(1);not null;default:0;comment:Sell"`
	Sort              int64     `gorm:"type:int;not null;default:0;comment:Sort"`
	DeductionRatio    int64     `gorm:"type:int;default:0;comment:Deduction Ratio"`
	AllowDeduction    *bool     `gorm:"type:tinyint(1);default:1;comment:Allow deduction"`
	ResetCycle        int64     `gorm:"type:int;default:0;comment:Reset Cycle: 0: No Reset, 1: 1st, 2: Monthly, 3: Yearly"`
	RenewalReset      *bool     `gorm:"type:tinyint(1);default:0;comment:Renew Reset"`
	ShowOriginalPrice bool      `gorm:"type:tinyint(1);not null;default:1;comment:Show Original Price"`
	CreatedAt         time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt         time.Time `gorm:"comment:Update Time"`
}

func (*Subscribe) TableName() string {
	return "subscribe"
}

func (s *Subscribe) BeforeCreate(tx *gorm.DB) error {
	if s.Sort == 0 {
		var maxSort int64
		if err := tx.Model(&Subscribe{}).Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		s.Sort = maxSort + 1
	}
	return nil
}

func (s *Subscribe) BeforeDelete(tx *gorm.DB) error {
	if err := tx.Exec("UPDATE subscribe SET sort = sort - 1 WHERE sort > ?", s.Sort).Error; err != nil {
		return err
	}
	return nil
}
func (s *Subscribe) BeforeUpdate(tx *gorm.DB) error {
	var count int64
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Model(&Subscribe{}).
		Where("sort = ? AND id != ?", s.Sort, s.Id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		var maxSort int64
		if err := tx.Model(&Subscribe{}).Select("MAX(sort)").Scan(&maxSort).Error; err != nil {
			return err
		}
		s.Sort = maxSort + 1
	}
	return nil
}

type Discount struct {
	Months   int64 `json:"months"`
	Discount int64 `json:"discount"`
}

type Group struct {
	Id          int64     `gorm:"primaryKey"`
	Name        string    `gorm:"type:varchar(255);not null;default:'';comment:Group Name"`
	Description string    `gorm:"type:text;comment:Group Description"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt   time.Time `gorm:"comment:Update Time"`
}

func (Group) TableName() string {
	return "subscribe_group"
}

const MaxSubscribePageSize = 100

// FilterParams subscribe 列表过滤参数
type FilterParams struct {
	Page            int      // Page Number
	Size            int      // Page Size
	Ids             []int64  // Subscribe IDs
	Node            []int64  // Node IDs
	Tags            []string // Node Tags
	Show            bool     // Show Portal Page
	Sell            bool     // Sell
	Language        string   // Language
	DefaultLanguage bool     // Default Subscribe Language Data
	Search          string   // Search Keywords
}

func (p *FilterParams) Normalize() {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.Size <= 0 {
		p.Size = 10
	}
	if p.Size > MaxSubscribePageSize {
		p.Size = MaxSubscribePageSize
	}
}
