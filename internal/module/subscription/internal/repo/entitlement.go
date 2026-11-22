package repo

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/module/subscription/entity/entitlement"
	"github.com/perfect-panel/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type entitlementRepo struct{ db *gorm.DB }

func (r *entitlementRepo) PlanIDs(ctx context.Context, entitlementID string) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&entitlement.Period{}).Where("entitlement_id = ?", entitlementID).Distinct("plan_id").Pluck("plan_id", &ids).Error
	return ids, err
}

func (r *entitlementRepo) InsertRevision(ctx context.Context, revision *entitlement.Revision) error {
	return r.db.WithContext(ctx).Create(revision).Error
}

func NewEntitlementRepo(db *gorm.DB) repository.EntitlementRepo { return &entitlementRepo{db: db} }

func (r *entitlementRepo) FindStateForUpdate(ctx context.Context, id string) (*entitlement.State, error) {
	var state entitlement.State
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&state, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &state, err
}
func (r *entitlementRepo) InsertState(ctx context.Context, state *entitlement.State) error {
	return r.db.WithContext(ctx).Create(state).Error
}
func (r *entitlementRepo) UpdateState(ctx context.Context, state *entitlement.State) error {
	return r.db.WithContext(ctx).Model(state).Select("*").Updates(state).Error
}
func (r *entitlementRepo) FindPeriod(ctx context.Context, id string) (*entitlement.Period, error) {
	var period entitlement.Period
	err := r.db.WithContext(ctx).First(&period, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &period, err
}
func (r *entitlementRepo) InsertPeriod(ctx context.Context, period *entitlement.Period) error {
	return r.db.WithContext(ctx).Create(period).Error
}
func (r *entitlementRepo) UpdatePeriod(ctx context.Context, period *entitlement.Period) error {
	return r.db.WithContext(ctx).Model(period).Select("*").Updates(period).Error
}
