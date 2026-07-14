package ticket

import "time"

const (
	Pending   = 1 // Pending  # Pending follow up
	Waiting   = 2 // Waiting  # Waiting for user response
	Processed = 3 // Processed
	Closed    = 4 // Closed
)

type Ticket struct {
	Id          int64     `gorm:"primaryKey"`
	Title       string    `gorm:"type:varchar(255);not null;default:'';comment:Title"`
	Description string    `gorm:"type:text;comment:Description"`
	UserId      int64     `gorm:"type:bigint;not null;default:0;comment:UserId"`
	Status      uint8     `gorm:"type:tinyint(1);not null;default:1;comment:Status"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt   time.Time `gorm:"comment:Update Time"`
}

func (Ticket) TableName() string {
	return "ticket"
}

type Follow struct {
	Id        int64     `gorm:"primaryKey"`
	TicketId  int64     `gorm:"type:bigint;not null;default:0;comment:TicketId"`
	From      string    `gorm:"type:varchar(255);not null;default:'';comment:From"`
	Type      uint8     `gorm:"type:tinyint(1);not null;default:1;comment:Type: 1 text, 2 image"`
	Content   string    `gorm:"type:text;comment:Content"`
	CreatedAt time.Time `gorm:"<-:create;comment:Create Time"`
}

func (Follow) TableName() string {
	return "ticket_follow"
}

// Details 是工单详情视图（含 Follows 预加载），仅做数据类型保留在 model 层。
type Details struct {
	Id          int64     `gorm:"primaryKey"`
	Title       string    `gorm:"type:varchar(255);not null;default:'';comment:Title"`
	Description string    `gorm:"type:text;comment:Description"`
	UserId      int64     `gorm:"type:bigint;not null;default:0;comment:UserId"`
	Status      uint8     `gorm:"type:tinyint(1);not null;default:1;comment:Status"`
	Follows     []Follow  `gorm:"foreignKey:TicketId;references:Id"`
	CreatedAt   time.Time `gorm:"<-:create;comment:Create Time"`
	UpdatedAt   time.Time `gorm:"comment:Update Time"`
}
