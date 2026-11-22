package repo

import (
	"context"
	"time"

	"github.com/perfect-panel/backend/internal/module/platform/entity/inbox"
	"gorm.io/gorm/clause"
)

// Identical retries share the receipt even if the first INSERT committed but
// the response was lost. Reusing the key with different traffic is an error.
func (m *inboxRepo) AdmitTrafficReport(ctx context.Context, r *inbox.TrafficReport) error {
	db := m.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "report_key"}}, DoNothing: true}).Create(r).Error; err != nil {
		return err
	}
	var stored inbox.TrafficReport
	if err := db.Select("digest").Where("report_key = ?", r.Key).Take(&stored).Error; err != nil {
		return err
	}
	if stored.Digest != r.Digest {
		return inbox.ErrTrafficReportConflict
	}
	return nil
}

func (m *inboxRepo) PendingTrafficReports(ctx context.Context, after int64, through time.Time, limit int) ([]inbox.TrafficReport, error) {
	var rows []inbox.TrafficReport
	err := m.db.WithContext(ctx).Where("completed = ? AND id > ? AND received_at <= ?", false, after, through).Order("id").Limit(limit).Find(&rows).Error
	return rows, err
}

// These flags and their corresponding domain writes commit in the same local
// transaction. A second worker or a retry after an ambiguous commit sees the
// spent flag and skips the effect. Ordinary inbox retention cannot undo them.
func (m *inboxRepo) ClaimTrafficReportUsage(ctx context.Context, key string) (bool, error) {
	r := m.db.WithContext(ctx).Model(&inbox.TrafficReport{}).Where("report_key = ? AND usage_applied = ? AND completed = ?", key, false, false).Update("usage_applied", true)
	return r.RowsAffected == 1, r.Error
}

func (m *inboxRepo) FinishTrafficReport(ctx context.Context, key string) (bool, error) {
	r := m.db.WithContext(ctx).Model(&inbox.TrafficReport{}).Where("report_key = ? AND usage_applied = ? AND completed = ?", key, true, false).Updates(map[string]any{"payload": nil, "completed": true})
	return r.RowsAffected == 1, r.Error
}
