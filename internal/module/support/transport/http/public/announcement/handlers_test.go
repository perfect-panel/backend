package announcement

import (
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	announcementEntity "github.com/perfect-panel/backend/internal/module/support/entity/announcement"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The user announcement list runs against the real support facade over the
// harness database: a published popup (1), a draft (2) and a published,
// pinned welcome (3).

func boardFixture(t *testing.T) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	env.Announcement(t, announcementEntity.Announcement{Title: "maintenance", Content: "tonight at 2am", Show: new(true), Popup: new(true)})
	env.Announcement(t, announcementEntity.Announcement{Title: "new plans", Content: "coming soon"})
	env.Announcement(t, announcementEntity.Announcement{Title: "welcome", Content: "read the guides", Show: new(true), Pinned: new(true)})
	h := server.New()
	h.GET("/v1/public/announcement/list", QueryAnnouncementHandler(support.New(support.Deps{Announcements: env.Announcements})))
	return env, h
}

// Users see only published announcements, filtered as asked; no parameter
// reveals a draft.
func TestAnnouncementListShowsPublishedAnnouncements(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		total       int64
		ids         []int64
	}{
		{"all", "page=1&size=10", 2, []int64{1, 3}},
		{"drafts asked for", "page=1&size=10&show=false&search=plans", 2, []int64{1, 3}},
		{"pinned", "page=1&size=10&pinned=true", 1, []int64{3}},
		{"not pinned", "page=1&size=10&pinned=false", 1, []int64{1}},
		{"popups", "page=1&size=10&popup=true", 1, []int64{1}},
		{"second page", "page=2&size=1", 2, []int64{3}},
		{"past the end", "page=3&size=1", 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := boardFixture(t)
			want := dto.QueryAnnouncementResponse{Total: tc.total, List: []dto.Announcement{}}
			for _, id := range tc.ids {
				want.List = append(want.List, supporttest.AnnouncementView(env.ReloadAnnouncement(t, id)))
			}
			// The list is answered under "announcements".
			reply := supporttest.Serve(t, h, http.MethodGet, "/v1/public/announcement/list?"+tc.query, "")
			reply.OK(t, want)
		})
	}
}

func TestAnnouncementListRefusesMalformedQueries(t *testing.T) {
	for _, tc := range []struct{ name, query, msg string }{
		{"page not a number", "page=first&size=10", "bind Page"},
		{"pinned not a switch", "page=1&size=10&pinned=yes", "bind Pinned"},
		{"without page", "size=10", "Page is a required field"},
		{"size too large", "page=1&size=101", "Size must be 100 or less"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h := boardFixture(t)
			supporttest.Serve(t, h, http.MethodGet, "/v1/public/announcement/list?"+tc.query, "").Refused(t, xerr.InvalidParams, tc.msg)
		})
	}
}
