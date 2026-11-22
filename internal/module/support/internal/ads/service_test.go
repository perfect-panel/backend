package ads

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The ads service runs over the harness repository. Three of the four ads
// are enabled; every one of them runs now.
func newCampaigns(t *testing.T) (*supporttest.Env, *Service) {
	t.Helper()
	logtest.Discard(t)
	env := supporttest.New(t)
	now := time.Now()
	for _, row := range []entity.Ads{
		{Title: "spring sale", Content: "20% off everything", Status: 1},
		{Title: "summer sale", Content: "30% off", Status: 0},
		{Title: "black friday", Content: "half price sale", Status: 1},
		{Title: "new app", Content: "download now", Status: 1},
	} {
		row.StartTime, row.EndTime = now.Add(-time.Hour), now.Add(time.Hour)
		env.Ad(t, row)
	}
	return env, NewService(env.Ads)
}

func ids(list []dto.Ads) []int {
	out := []int{}
	for _, item := range list {
		out = append(out, item.Id)
	}
	return out
}

func status(v int) *int { return &v }

// The admin list narrows by status and by a search of title and content,
// and pages with the total of every match.
func TestAdminListFiltersAndPages(t *testing.T) {
	env, svc := newCampaigns(t)
	for _, tc := range []struct {
		name  string
		req   dto.GetAdsListRequest
		total int64
		ids   []int
	}{
		{"all", dto.GetAdsListRequest{Page: 1, Size: 10}, 4, []int{1, 2, 3, 4}},
		{"enabled", dto.GetAdsListRequest{Page: 1, Size: 10, Status: status(1)}, 3, []int{1, 3, 4}},
		{"disabled", dto.GetAdsListRequest{Page: 1, Size: 10, Status: status(0)}, 1, []int{2}},
		{"search in titles and contents", dto.GetAdsListRequest{Page: 1, Size: 10, Search: "sale"}, 3, []int{1, 2, 3}},
		{"enabled search", dto.GetAdsListRequest{Page: 1, Size: 10, Status: status(1), Search: "sale"}, 2, []int{1, 3}},
		{"second page", dto.GetAdsListRequest{Page: 2, Size: 3}, 4, []int{4}},
		{"past the end", dto.GetAdsListRequest{Page: 3, Size: 3}, 4, []int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.List(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			// No match is an empty list, not null.
			if resp.Total != tc.total || resp.List == nil || !reflect.DeepEqual(ids(resp.List), tc.ids) {
				t.Fatalf("list = %d %v, want %d %v", resp.Total, ids(resp.List), tc.total, tc.ids)
			}
			for _, item := range resp.List {
				if want := supporttest.AdView(env.ReloadAd(t, int64(item.Id))); item != want {
					t.Fatalf("ad = %+v, want %+v", item, want)
				}
			}
		})
	}
}

// An ad's schedule round-trips in Unix milliseconds: what the admin panel
// wrote is what the detail and the public list show.
func TestAdScheduleRoundTripsInMilliseconds(t *testing.T) {
	_, svc := newCampaigns(t)
	ctx := context.Background()
	start, end := time.Now().Add(-time.Minute).Truncate(time.Millisecond), time.Now().Add(time.Hour).Truncate(time.Millisecond)
	if err := svc.Create(ctx, &dto.CreateAdsRequest{Title: "launch", Type: "banner", Content: "c", Description: "d", TargetURL: "https://panel.example",
		StartTime: start.UnixMilli(), EndTime: end.UnixMilli(), Status: 1}); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.GetDetail(ctx, &dto.GetAdsDetailRequest{Id: 5})
	if err != nil || detail.StartTime != start.UnixMilli() || detail.EndTime != end.UnixMilli() || detail.Type != "banner" || detail.TargetURL != "https://panel.example" {
		t.Fatalf("detail = %+v (err %v), want the schedule as written", detail, err)
	}
	public, err := svc.GetPublicAds(ctx, &dto.GetAdsRequest{})
	if err != nil || !reflect.DeepEqual(ids(public.List), []int{1, 3, 4, 5}) {
		t.Fatalf("public ads = %v (err %v), want the enabled ones with the new ad", ids(public.List), err)
	}
	// Rescheduled into the future, the ad leaves the public list.
	if err := svc.Update(ctx, &dto.UpdateAdsRequest{Id: 5, Title: "launch", Status: 1, StartTime: end.UnixMilli(), EndTime: end.Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	if public, err := svc.GetPublicAds(ctx, &dto.GetAdsRequest{}); err != nil || !reflect.DeepEqual(ids(public.List), []int{1, 3, 4}) {
		t.Fatalf("public ads = %v (err %v), want the rescheduled ad gone", ids(public.List), err)
	}
}

func TestMissingAdIsNotFound(t *testing.T) {
	_, svc := newCampaigns(t)
	ctx := context.Background()
	if _, err := svc.GetDetail(ctx, &dto.GetAdsDetailRequest{Id: 9}); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("detail: %v, want not found", err)
	}
	if err := svc.Update(ctx, &dto.UpdateAdsRequest{Id: 9, Title: "ghost"}); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("update: %v, want not found", err)
	}
	// Deleting it succeeds: it is gone either way.
	if err := svc.Delete(ctx, &dto.DeleteAdsRequest{Id: 9}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// A statement the store refuses is reported under the failing operation's
// code, and the ads stay as they were.
func TestAdsStoreFailures(t *testing.T) {
	refused := errors.New("disk full")
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		op   string
		run  func(s *Service) error
		code uint32
	}{
		{"create", "create", func(s *Service) error { return s.Create(ctx, &dto.CreateAdsRequest{Title: "t"}) }, xerr.DatabaseInsertError},
		{"update", "update", func(s *Service) error { return s.Update(ctx, &dto.UpdateAdsRequest{Id: 1, Title: "t"}) }, xerr.DatabaseUpdateError},
		{"delete", "delete", func(s *Service) error { return s.Delete(ctx, &dto.DeleteAdsRequest{Id: 1}) }, xerr.DatabaseDeletedError},
		{"admin list", "query", func(s *Service) error {
			_, err := s.List(ctx, &dto.GetAdsListRequest{Page: 1, Size: 10})
			return err
		}, xerr.DatabaseQueryError},
		// The public list passes the store's error on as it is.
		{"public list", "query", func(s *Service) error {
			_, err := s.GetPublicAds(ctx, &dto.GetAdsRequest{})
			return err
		}, xerr.ERROR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc := newCampaigns(t)
			lift := env.Refuse(t, tc.op, "ads", 0, refused)
			err := tc.run(svc)
			lift()
			if xerr.CodeOf(err) != tc.code || !errors.Is(err, refused) {
				t.Fatalf("error = %v, want code %d wrapping the refusal", err, tc.code)
			}
			if got := env.ReloadAd(t, 1); got.Title != "spring sale" || env.Count(t, &entity.Ads{}) != 4 {
				t.Fatalf("ad = %+v, want the ads unchanged", got)
			}
		})
	}
}
