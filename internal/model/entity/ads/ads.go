package ads

import "time"

type Ads struct {
	Id          int64     `gorm:"primaryKey"`
	Title       string    `gorm:"type:varchar(255);default:'';not null;comment:Ads title"`
	Type        string    `gorm:"type:varchar(255);default:'';not null;comment:Ads type"`
	Content     string    `gorm:"type:text;comment:Ads content"`
	Description string    `gorm:"type:text;comment:Ads descriptor"`
	TargetURL   string    `gorm:"type:varchar(512);default:'';comment:Ads target url"`
	StartTime   time.Time `gorm:"type:datetime;comment:Ads start time"`
	EndTime     time.Time `gorm:"type:datetime;comment:Ads end time"`
	Status      int       `gorm:"type:TINYINT;default:0;comment:Ads status,0 disable,1 enable"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt   time.Time `gorm:"comment:Update Time"`
}

func (Ads) TableName() string {
	return "ads"
}

// Filter ads 列表查询过滤条件
type Filter struct {
	Status *int
	Search string
}
