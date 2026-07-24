package inbox

import "time"

// Record marks a domain event as processed by one consuming domain step.
// Inserting it inside the consumer's transaction makes at-least-once event
// deliveries idempotent (idempotent-consumer/inbox pattern, ADR-001 step 2).
type Record struct {
	Consumer    string    `gorm:"primaryKey;type:varchar(64);not null;comment:Consuming domain step"`
	EventKey    string    `gorm:"primaryKey;type:varchar(191);not null;comment:Business key of the event"`
	Result      string    `gorm:"type:varchar(255);not null;default:'';comment:Optional outcome needed by later steps"`
	ProcessedAt time.Time `gorm:"<-:create;autoCreateTime"`
}

func (Record) TableName() string {
	return "domain_event_inbox"
}
