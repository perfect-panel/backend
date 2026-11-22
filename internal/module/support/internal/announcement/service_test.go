package announcement

import (
	"context"
	"errors"
	"reflect"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The announcement service runs over the harness repository. The board holds
// three published announcements (1, 3, 4) and two drafts (2, 5).
func newBoard(t *testing.T) (*supporttest.Env, *Service) {
	t.Helper()
	logtest.Discard(t)
	env := supporttest.New(t)
	for _, row := range []entity.Announcement{
		{Title: "maintenance", Content: "tonight at 2am", Show: new(true), Popup: new(true)},
		{Title: "new plans", Content: "coming soon"},
		{Title: "welcome", Content: "read the guides first", Show: new(true), Pinned: new(true)},
		{Title: "holiday hours", Content: "closed on friday", Show: new(true), Pinned: new(true), Popup: new(true)},
		{Title: "old news", Content: "no longer true", Pinned: new(true)},
	} {
		env.Announcement(t, row)
	}
	return env, NewService(env.Announcements)
}

func ids(list []dto.Announcement) []int64 {
	out := []int64{}
	for _, item := range list {
		out = append(out, item.Id)
	}
	return out
}

// The admin list narrows by each switch and by a search of title and
// content, and pages with the total of every match.
func TestAdminListFiltersAndPages(t *testing.T) {
	_, svc := newBoard(t)
	for _, tc := range []struct {
		name  string
		req   dto.GetAnnouncementListRequest
		total int64
		ids   []int64
	}{
		{"all", dto.GetAnnouncementListRequest{Page: 1, Size: 10}, 5, []int64{1, 2, 3, 4, 5}},
		{"published", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Show: new(true)}, 3, []int64{1, 3, 4}},
		{"drafts", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Show: new(false)}, 2, []int64{2, 5}},
		{"pinned", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Pinned: new(true)}, 3, []int64{3, 4, 5}},
		{"published popups", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Show: new(true), Popup: new(true)}, 2, []int64{1, 4}},
		{"not popups", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Popup: new(false)}, 3, []int64{2, 3, 5}},
		{"search in titles", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Search: "news"}, 1, []int64{5}},
		{"search in contents", dto.GetAnnouncementListRequest{Page: 1, Size: 10, Search: "friday"}, 1, []int64{4}},
		{"second page", dto.GetAnnouncementListRequest{Page: 2, Size: 2}, 5, []int64{3, 4}},
		{"past the end", dto.GetAnnouncementListRequest{Page: 4, Size: 2}, 5, []int64{}},
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
		})
	}
}

// Users page through the published announcements only, narrowed by the
// pinned and popup switches.
func TestVisibleListShowsPublishedAnnouncementsOnly(t *testing.T) {
	env, svc := newBoard(t)
	for _, tc := range []struct {
		name  string
		req   dto.QueryAnnouncementRequest
		total int64
		ids   []int64
	}{
		{"published", dto.QueryAnnouncementRequest{Page: 1, Size: 10}, 3, []int64{1, 3, 4}},
		{"pinned", dto.QueryAnnouncementRequest{Page: 1, Size: 10, Pinned: new(true)}, 2, []int64{3, 4}},
		{"not pinned", dto.QueryAnnouncementRequest{Page: 1, Size: 10, Pinned: new(false)}, 1, []int64{1}},
		{"popups", dto.QueryAnnouncementRequest{Page: 1, Size: 10, Popup: new(true)}, 2, []int64{1, 4}},
		{"second page", dto.QueryAnnouncementRequest{Page: 2, Size: 2}, 3, []int64{4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.QueryVisible(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Total != tc.total || !reflect.DeepEqual(ids(resp.List), tc.ids) {
				t.Fatalf("list = %d %v, want %d %v", resp.Total, ids(resp.List), tc.total, tc.ids)
			}
			for _, item := range resp.List {
				if want := supporttest.AnnouncementView(env.ReloadAnnouncement(t, item.Id)); !reflect.DeepEqual(item, want) {
					t.Fatalf("announcement = %+v, want %+v", item, want)
				}
			}
		})
	}
}

// A new announcement is a draft, and an update replaces the text while the
// switches it leaves out keep their values.
func TestAnnouncementsAreWrittenAsDrafts(t *testing.T) {
	env, svc := newBoard(t)
	ctx := context.Background()
	if err := svc.Create(ctx, &dto.CreateAnnouncementRequest{Title: "price change", Content: "from next month"}); err != nil {
		t.Fatal(err)
	}
	created := env.ReloadAnnouncement(t, 6)
	if created.Title != "price change" || *created.Show || *created.Pinned || *created.Popup {
		t.Fatalf("created = %+v, want a draft", created)
	}
	if err := svc.Update(ctx, &dto.UpdateAnnouncementRequest{Id: 4, Title: "holiday hours", Content: "open on friday", Popup: new(false)}); err != nil {
		t.Fatal(err)
	}
	updated := env.ReloadAnnouncement(t, 4)
	if updated.Content != "open on friday" || !*updated.Show || !*updated.Pinned || *updated.Popup {
		t.Fatalf("updated = %+v, want the text replaced, still published and pinned, no popup", updated)
	}
	if err := svc.Update(ctx, &dto.UpdateAnnouncementRequest{Id: 2, Title: "new plans", Content: "available now", Show: new(true), Pinned: new(true)}); err != nil {
		t.Fatal(err)
	}
	if published := env.ReloadAnnouncement(t, 2); !*published.Show || !*published.Pinned || *published.Popup {
		t.Fatalf("published = %+v, want the draft published and pinned, still no popup", published)
	}
	got, err := svc.Get(ctx, &dto.GetAnnouncementRequest{Id: 4})
	if err != nil || !reflect.DeepEqual(*got, supporttest.AnnouncementView(updated)) {
		t.Fatalf("detail = %+v (err %v), want the stored announcement", got, err)
	}
	if err := svc.Delete(ctx, &dto.DeleteAnnouncementRequest{Id: 4}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, &dto.GetAnnouncementRequest{Id: 4}); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted announcement: %v, want not found", err)
	}
	// Deleting it again succeeds: it is gone either way.
	if err := svc.Delete(ctx, &dto.DeleteAnnouncementRequest{Id: 4}); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

// A statement the store refuses is reported under the failing operation's
// code, and the announcement stays as it was.
func TestAnnouncementStoreFailures(t *testing.T) {
	refused := errors.New("disk full")
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		op   string
		skip int
		run  func(s *Service) error
		code uint32
	}{
		{"create", "create", 0, func(s *Service) error {
			return s.Create(ctx, &dto.CreateAnnouncementRequest{Title: "t", Content: "c"})
		}, xerr.DatabaseInsertError},
		{"update, unreadable", "query", 0, func(s *Service) error {
			return s.Update(ctx, &dto.UpdateAnnouncementRequest{Id: 1, Title: "t", Content: "c"})
		}, xerr.DatabaseQueryError},
		{"update, refused", "update", 0, func(s *Service) error {
			return s.Update(ctx, &dto.UpdateAnnouncementRequest{Id: 1, Title: "t", Content: "c"})
		}, xerr.DatabaseUpdateError},
		{"delete", "delete", 0, func(s *Service) error {
			return s.Delete(ctx, &dto.DeleteAnnouncementRequest{Id: 1})
		}, xerr.DatabaseDeletedError},
		{"admin list", "query", 0, func(s *Service) error {
			_, err := s.List(ctx, &dto.GetAnnouncementListRequest{Page: 1, Size: 10})
			return err
		}, xerr.DatabaseQueryError},
		{"visible list", "query", 0, func(s *Service) error {
			_, err := s.QueryVisible(ctx, &dto.QueryAnnouncementRequest{Page: 1, Size: 10})
			return err
		}, xerr.DatabaseQueryError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc := newBoard(t)
			lift := env.Refuse(t, tc.op, "announcement", tc.skip, refused)
			err := tc.run(svc)
			lift()
			if xerr.CodeOf(err) != tc.code || !errors.Is(err, refused) {
				t.Fatalf("error = %v, want code %d wrapping the refusal", err, tc.code)
			}
			if got := env.ReloadAnnouncement(t, 1); got.Title != "maintenance" || env.Count(t, &entity.Announcement{}) != 5 {
				t.Fatalf("announcement = %+v, want the board unchanged", got)
			}
		})
	}
}
