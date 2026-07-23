package order

import "time"

// Event is the durable order event outbox.  Redis only distributes these
// records with low latency; reconnecting clients always recover from this
// table.
type Event struct {
	ID          int64      `gorm:"primaryKey;autoIncrement"`
	OrderID     int64      `gorm:"type:bigint;not null;index:idx_order_event_order_id_id,priority:1"`
	OrderNo     string     `gorm:"type:varchar(255);not null;index:idx_order_event_order_no_id,priority:1"`
	EventType   string     `gorm:"type:varchar(64);not null"`
	Payload     string     `gorm:"type:text;not null"`
	CreatedAt   time.Time  `gorm:"<-:create"`
	PublishedAt *time.Time `gorm:"index:idx_order_event_published_at_id,priority:1"`
}

func (Event) TableName() string { return "order_event" }
