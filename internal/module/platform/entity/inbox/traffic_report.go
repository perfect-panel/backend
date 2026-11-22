package inbox

import (
	"errors"
	"time"
)

var ErrTrafficReportConflict = errors.New("traffic report ID already identifies a different batch")

// TrafficReport is the durable receipt for one immutable, node-identified traffic
// batch. Payload contains the admitted, scaled deltas until both consumers
// finish; the small receipt remains to acknowledge late retries.
type TrafficReport struct {
	ID           int64     `gorm:"primaryKey;autoIncrement"`
	Key          string    `gorm:"column:report_key;size:64;not null;uniqueIndex"`
	Digest       string    `gorm:"size:64;not null"`
	ReceivedAt   time.Time `gorm:"not null"`
	Payload      []byte
	UsageApplied bool `gorm:"not null"`
	Completed    bool `gorm:"not null;index:idx_traffic_report_pending,priority:1"`
}

func (TrafficReport) TableName() string { return "node_traffic_report" }
