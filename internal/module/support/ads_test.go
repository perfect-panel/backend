package support_test

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	adsEntity "github.com/perfect-panel/server/internal/model/entity/ads"
	"github.com/perfect-panel/server/internal/module/support"
)

type fakeAdsRepo struct {
	inserted   *adsEntity.Ads
	updated    *adsEntity.Ads
	findOne    *adsEntity.Ads
	listFilter adsEntity.Filter
}

func (f *fakeAdsRepo) Insert(_ context.Context, data *adsEntity.Ads) error {
	f.inserted = data
	return nil
}

func (f *fakeAdsRepo) FindOne(_ context.Context, _ int64) (*adsEntity.Ads, error) {
	return f.findOne, nil
}

func (f *fakeAdsRepo) Update(_ context.Context, data *adsEntity.Ads) error {
	f.updated = data
	return nil
}

func (f *fakeAdsRepo) Delete(_ context.Context, _ int64) error { return nil }

func (f *fakeAdsRepo) GetAdsListByPage(_ context.Context, _, _ int, filter adsEntity.Filter) (int64, []*adsEntity.Ads, error) {
	f.listFilter = filter
	return 0, nil, nil
}

func newAdsService(repo *fakeAdsRepo) support.Service {
	return support.New(support.Deps{Ads: repo})
}

func TestCreateAdsConvertsMilliTimestamps(t *testing.T) {
	repo := &fakeAdsRepo{}
	svc := newAdsService(repo)

	start := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	err := svc.CreateAds(context.Background(), &dto.CreateAdsRequest{
		Title: "t", StartTime: start.UnixMilli(), EndTime: end.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CreateAds: %v", err)
	}
	if repo.inserted == nil || !repo.inserted.StartTime.Equal(start) || !repo.inserted.EndTime.Equal(end) {
		t.Fatalf("timestamps not converted: %+v", repo.inserted)
	}
}

func TestUpdateAdsOverwritesTimesAfterCopy(t *testing.T) {
	repo := &fakeAdsRepo{findOne: &adsEntity.Ads{
		Id: 5, Title: "old", StartTime: time.Unix(0, 0), EndTime: time.Unix(0, 0),
	}}
	svc := newAdsService(repo)

	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	err := svc.UpdateAds(context.Background(), &dto.UpdateAdsRequest{
		Id: 5, Title: "new", StartTime: start.UnixMilli(), EndTime: end.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("UpdateAds: %v", err)
	}
	got := repo.updated
	if got == nil || got.Title != "new" {
		t.Fatalf("update not applied: %+v", got)
	}
	if !got.StartTime.Equal(start) || !got.EndTime.Equal(end) {
		t.Fatalf("times must come from the request, not DeepCopy: start=%v end=%v", got.StartTime, got.EndTime)
	}
}
