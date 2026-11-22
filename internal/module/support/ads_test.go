package support_test

import (
	"context"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	adsEntity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
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
		Title: "t", Description: "d", StartTime: start.UnixMilli(), EndTime: end.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("CreateAds: %v", err)
	}
	if repo.inserted == nil || !repo.inserted.StartTime.Equal(start) || !repo.inserted.EndTime.Equal(end) {
		t.Fatalf("timestamps not converted: %+v", repo.inserted)
	}
	// The description used to be dropped on create; only an update kept it.
	if repo.inserted.Description != "d" {
		t.Fatalf("description = %q, want the request's", repo.inserted.Description)
	}
}

// The public list must honour the schedule, not only the enabled flag.
func TestGetPublicAdsRequiresEnabledAndScheduled(t *testing.T) {
	repo := &fakeAdsRepo{}
	svc := newAdsService(repo)

	before := time.Now()
	if _, err := svc.GetPublicAds(context.Background(), &dto.GetAdsRequest{}); err != nil {
		t.Fatalf("GetPublicAds: %v", err)
	}
	filter := repo.listFilter
	if filter.Status == nil || *filter.Status != 1 {
		t.Fatalf("status filter = %v, want enabled ads only", filter.Status)
	}
	if filter.ActiveAt == nil || filter.ActiveAt.Before(before) || filter.ActiveAt.After(time.Now()) {
		t.Fatalf("schedule filter = %v, want the current time", filter.ActiveAt)
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
		t.Fatalf("times must come from the request: start=%v end=%v", got.StartTime, got.EndTime)
	}
}

// The detail shows every field of the ad, its times in Unix milliseconds.
func TestGetAdsDetailMapsEntity(t *testing.T) {
	start := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeAdsRepo{findOne: &adsEntity.Ads{
		Id: 5, Title: "t", Type: "banner", Content: "c", Description: "d", TargetURL: "https://example.com",
		StartTime: start, EndTime: start.Add(time.Hour), Status: 1,
		CreatedAt: start.Add(-time.Hour), UpdatedAt: start.Add(-time.Minute),
	}}

	got, err := newAdsService(repo).GetAdsDetail(context.Background(), &dto.GetAdsDetailRequest{Id: 5})
	if err != nil {
		t.Fatalf("GetAdsDetail: %v", err)
	}
	want := dto.Ads{
		Id: 5, Title: "t", Type: "banner", Content: "c", Description: "d", TargetURL: "https://example.com",
		StartTime: start.UnixMilli(), EndTime: start.Add(time.Hour).UnixMilli(), Status: 1,
		CreatedAt: start.Add(-time.Hour).UnixMilli(), UpdatedAt: start.Add(-time.Minute).UnixMilli(),
	}
	if *got != want {
		t.Fatalf("detail = %+v, want %+v", *got, want)
	}
}
