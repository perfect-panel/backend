package node

import "time"

type ServerConfigOverride struct {
	Id         int64     `gorm:"primary_key"`
	ServerId   int64     `gorm:"uniqueIndex;not null;comment:Server ID"`
	IPStrategy *string   `gorm:"type:varchar(32);comment:IP strategy override, NULL means inherit"`
	DNS        *string   `gorm:"type:text;comment:DNS override, NULL means inherit"`
	Block      *string   `gorm:"type:text;comment:Block override, NULL means inherit"`
	Outbound   *string   `gorm:"type:text;comment:Outbound override, NULL means inherit"`
	CreatedAt  time.Time `gorm:"<-:create;comment:Creation Time"`
	UpdatedAt  time.Time `gorm:"comment:Update Time"`
}

func (*ServerConfigOverride) TableName() string {
	return "server_config_overrides"
}
