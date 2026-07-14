package announcement

import "time"

type Announcement struct {
	Id        int64     `gorm:"primaryKey"`
	Title     string    `gorm:"type:varchar(255);not null;default:'';comment:Title"`
	Content   string    `gorm:"type:text;comment:Content"`
	Show      *bool     `gorm:"type:tinyint(1);not null;default:0;comment:Show"`
	Pinned    *bool     `gorm:"type:tinyint(1);not null;default:0;comment:Pinned"`
	Popup     *bool     `gorm:"type:tinyint(1);not null;default:0;comment:Popup"`
	CreatedAt time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (Announcement) TableName() string {
	return "announcement"
}

// Filter announcement 列表查询过滤条件
type Filter struct {
	Show   *bool
	Pinned *bool
	Popup  *bool
	Search string
}
