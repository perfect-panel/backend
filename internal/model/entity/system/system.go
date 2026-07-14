package system

import "time"

type System struct {
	Id        int64     `gorm:"primarykey"`
	Category  string    `gorm:"type:varchar(100);default:'';not null;comment:Category"`
	Key       string    `gorm:"index:index_key;unique;type:varchar(100);default:'';not null;comment:Key Name"`
	Value     string    `gorm:"type:text;not null;comment:Key Value"`
	Type      string    `gorm:"type:varchar(50);default:'';not null;comment:Type"`
	Desc      string    `gorm:"type:text;not null;comment:Description"`
	CreatedAt time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (System) TableName() string {
	return "system"
}
