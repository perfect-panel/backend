package document

import "time"

type Document struct {
	Id        int64     `gorm:"primaryKey"`
	Title     string    `gorm:"type:varchar(255);not null;default:'';comment:Document Title"`
	Content   string    `gorm:"type:text;comment:Document Content"`
	Tags      string    `gorm:"type:varchar(255);not null;default:'';comment:Document Tags"`
	Show      *bool     `gorm:"type:tinyint(1);not null;default:1;comment:Show"`
	CreatedAt time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt time.Time `gorm:"comment:Update Time"`
}

func (Document) TableName() string {
	return "document"
}
