package coupon

import "time"

type Coupon struct {
	Id         int64     `gorm:"primaryKey"`
	Name       string    `gorm:"type:varchar(255);not null;default:'';comment:Coupon Name"`
	Code       string    `gorm:"type:varchar(255);not null;default:'';unique;comment:Coupon Code"`
	Count      int64     `gorm:"type:int;not null;default:0;comment:Count Limit"`
	Type       uint8     `gorm:"type:tinyint(1);not null;default:1;comment:Coupon Type: 1: Percentage 2: Fixed Amount"`
	Discount   int64     `gorm:"type:int;not null;default:0;comment:Coupon Discount"`
	StartTime  int64     `gorm:"type:int;not null;default:0;comment:Start Time"`
	ExpireTime int64     `gorm:"type:int;not null;default:0;comment:Expire Time"`
	UserLimit  int64     `gorm:"type:int;not null;default:0;comment:User Limit"`
	Subscribe  string    `gorm:"type:varchar(255);not null;default:'';comment:Subscribe Limit"`
	UsedCount  int64     `gorm:"type:int;not null;default:0;comment:Used Count"`
	Enable     *bool     `gorm:"type:tinyint(1);not null;default:1;comment:Enable"`
	CreatedAt  time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt  time.Time `gorm:"comment:Update Time"`
}

func (Coupon) TableName() string {
	return "coupon"
}

func (c *Coupon) IsEnabled() bool {
	return c != nil && c.Enable != nil && *c.Enable
}
