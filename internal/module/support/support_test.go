package support_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/announcement"
)

type fakeAnnouncementRepo struct {
	inserted   *entity.Announcement
	updated    *entity.Announcement
	deletedID  int64
	findOne    *entity.Announcement
	findErr    error
	listFilter entity.Filter
	listPage   int
	listSize   int
	list       []*entity.Announcement
	listTotal  int64
	listErr    error
}

func (f *fakeAnnouncementRepo) Insert(_ context.Context, data *entity.Announcement) error {
	f.inserted = data
	return nil
}

func (f *fakeAnnouncementRepo) FindOne(_ context.Context, _ int64) (*entity.Announcement, error) {
	return f.findOne, f.findErr
}

func (f *fakeAnnouncementRepo) Update(_ context.Context, data *entity.Announcement) error {
	f.updated = data
	return nil
}

func (f *fakeAnnouncementRepo) Delete(_ context.Context, id int64) error {
	f.deletedID = id
	return nil
}

func (f *fakeAnnouncementRepo) GetAnnouncementListByPage(_ context.Context, page, size int, filter entity.Filter) (int64, []*entity.Announcement, error) {
	f.listPage, f.listSize, f.listFilter = page, size, filter
	return f.listTotal, f.list, f.listErr
}

func ptr[T any](v T) *T { return &v }

func newService(repo *fakeAnnouncementRepo) support.Service {
	return support.New(support.Deps{Announcements: repo})
}

func TestCreateAnnouncementInsertsTitleAndContent(t *testing.T) {
	repo := &fakeAnnouncementRepo{}
	svc := newService(repo)

	if err := svc.CreateAnnouncement(context.Background(), &dto.CreateAnnouncementRequest{Title: "t", Content: "c"}); err != nil {
		t.Fatalf("CreateAnnouncement: %v", err)
	}
	if repo.inserted == nil || repo.inserted.Title != "t" || repo.inserted.Content != "c" {
		t.Fatalf("unexpected inserted entity: %+v", repo.inserted)
	}
}

func TestUpdateAnnouncementKeepsFlagsWhenOmitted(t *testing.T) {
	repo := &fakeAnnouncementRepo{findOne: &entity.Announcement{
		Id: 7, Title: "old", Content: "old", Show: ptr(true), Pinned: ptr(true), Popup: ptr(false),
	}}
	svc := newService(repo)

	err := svc.UpdateAnnouncement(context.Background(), &dto.UpdateAnnouncementRequest{
		Id: 7, Title: "new", Content: "new-content", Popup: ptr(true),
	})
	if err != nil {
		t.Fatalf("UpdateAnnouncement: %v", err)
	}
	got := repo.updated
	if got == nil {
		t.Fatal("Update was not called")
	}
	if got.Title != "new" || got.Content != "new-content" {
		t.Fatalf("title/content not updated: %+v", got)
	}
	if got.Show == nil || !*got.Show || got.Pinned == nil || !*got.Pinned {
		t.Fatalf("omitted flags must keep stored values: %+v", got)
	}
	if got.Popup == nil || !*got.Popup {
		t.Fatalf("provided flag must be applied: %+v", got)
	}
}

func TestUpdateAnnouncementPropagatesQueryError(t *testing.T) {
	repo := &fakeAnnouncementRepo{findErr: errors.New("boom")}
	svc := newService(repo)

	if err := svc.UpdateAnnouncement(context.Background(), &dto.UpdateAnnouncementRequest{Id: 1}); err == nil {
		t.Fatal("expected error when the announcement cannot be loaded")
	}
	if repo.updated != nil {
		t.Fatal("Update must not run after a failed load")
	}
}

func TestDeleteAnnouncementPassesID(t *testing.T) {
	repo := &fakeAnnouncementRepo{}
	svc := newService(repo)

	if err := svc.DeleteAnnouncement(context.Background(), &dto.DeleteAnnouncementRequest{Id: 42}); err != nil {
		t.Fatalf("DeleteAnnouncement: %v", err)
	}
	if repo.deletedID != 42 {
		t.Fatalf("deleted id = %d, want 42", repo.deletedID)
	}
}

func TestGetAnnouncementMapsEntity(t *testing.T) {
	created := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeAnnouncementRepo{findOne: &entity.Announcement{
		Id: 3, Title: "t", Content: "c", Show: ptr(true), Pinned: ptr(false),
		CreatedAt: created, UpdatedAt: created.Add(time.Minute),
	}}
	svc := newService(repo)

	got, err := svc.GetAnnouncement(context.Background(), &dto.GetAnnouncementRequest{Id: 3})
	if err != nil {
		t.Fatalf("GetAnnouncement: %v", err)
	}
	if got.Id != 3 || got.Title != "t" || got.Content != "c" || got.Show == nil || !*got.Show {
		t.Fatalf("unexpected dto: %+v", got)
	}
	if got.Pinned == nil || *got.Pinned || got.Popup != nil {
		t.Fatalf("flags = pinned %v, popup %v; want the stored ones", got.Pinned, got.Popup)
	}
	if got.CreatedAt != created.UnixMilli() || got.UpdatedAt != created.Add(time.Minute).UnixMilli() {
		t.Fatalf("times = %d/%d, want the stored ones in Unix milliseconds", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGetAnnouncementListForwardsAdminFilter(t *testing.T) {
	repo := &fakeAnnouncementRepo{listTotal: 1, list: []*entity.Announcement{{Id: 1, Title: "t"}}}
	svc := newService(repo)

	resp, err := svc.GetAnnouncementList(context.Background(), &dto.GetAnnouncementListRequest{
		Page: 2, Size: 10, Show: ptr(false), Search: "kw",
	})
	if err != nil {
		t.Fatalf("GetAnnouncementList: %v", err)
	}
	if repo.listPage != 2 || repo.listSize != 10 || repo.listFilter.Search != "kw" {
		t.Fatalf("filter not forwarded: page=%d size=%d filter=%+v", repo.listPage, repo.listSize, repo.listFilter)
	}
	if repo.listFilter.Show == nil || *repo.listFilter.Show {
		t.Fatalf("admin Show filter must pass through as-is: %+v", repo.listFilter.Show)
	}
	if resp.Total != 1 || len(resp.List) != 1 || resp.List[0].Title != "t" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestQueryAnnouncementForcesVisibleOnly(t *testing.T) {
	repo := &fakeAnnouncementRepo{}
	svc := newService(repo)

	if _, err := svc.QueryAnnouncement(context.Background(), &dto.QueryAnnouncementRequest{Page: 1, Size: 20, Pinned: ptr(true)}); err != nil {
		t.Fatalf("QueryAnnouncement: %v", err)
	}
	if repo.listFilter.Show == nil || !*repo.listFilter.Show {
		t.Fatal("public query must force Show=true")
	}
	if repo.listFilter.Search != "" {
		t.Fatal("public query must not expose search")
	}
	if repo.listFilter.Pinned == nil || !*repo.listFilter.Pinned {
		t.Fatalf("pinned filter not forwarded: %+v", repo.listFilter)
	}
}
