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

// The announcement handlers run against the real support facade over the
// harness database: each case starts from a published popup, "maintenance"
// (id 1), and a draft, "new plans" (id 2).

func announcementFixture(t *testing.T) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	env.Announcement(t, announcementEntity.Announcement{Title: "maintenance", Content: "tonight at 2am", Show: new(true), Popup: new(true)})
	env.Announcement(t, announcementEntity.Announcement{Title: "new plans", Content: "coming soon"})
	svc := support.New(support.Deps{Announcements: env.Announcements})
	h := server.New()
	group := h.Group("/v1/admin/announcement")
	group.POST("/", CreateAnnouncementHandler(svc))
	group.PUT("/", UpdateAnnouncementHandler(svc))
	group.DELETE("/", DeleteAnnouncementHandler(svc))
	group.GET("/detail", GetAnnouncementHandler(svc))
	group.GET("/list", GetAnnouncementListHandler(svc))
	return env, h
}

func flags(a announcementEntity.Announcement) [3]bool {
	return [3]bool{*a.Show, *a.Pinned, *a.Popup}
}

func TestAnnouncementHandlersServeTheStoredAnnouncements(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		check                      func(t *testing.T, env *supporttest.Env, reply supporttest.Reply)
	}{
		// A new announcement is a draft: hidden, not pinned, no popup.
		{"create", http.MethodPost, "/v1/admin/announcement/", `{"title":"price change","content":"from next month"}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadAnnouncement(t, 3)
				if got.Title != "price change" || got.Content != "from next month" || flags(got) != [3]bool{} {
					t.Fatalf("stored announcement = %+v, want a draft with the request's text", got)
				}
			}},
		// The flags the update omits keep their stored values.
		{"update", http.MethodPut, "/v1/admin/announcement/", `{"id":1,"title":"maintenance moved","content":"tomorrow at 2am","pinned":true}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadAnnouncement(t, 1)
				if got.Title != "maintenance moved" || got.Content != "tomorrow at 2am" || flags(got) != [3]bool{true, true, true} {
					t.Fatalf("stored announcement = %+v, want the text replaced, pinned, still shown as a popup", got)
				}
			}},
		{"update missing", http.MethodPut, "/v1/admin/announcement/", `{"id":9,"title":"ghost"}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
				if n := env.Count(t, &announcementEntity.Announcement{}); n != 2 {
					t.Fatalf("%d announcements, want none created", n)
				}
			}},
		{"delete", http.MethodDelete, "/v1/admin/announcement/", `{"id":2}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if n := env.Count(t, &announcementEntity.Announcement{}); n != 1 || env.ReloadAnnouncement(t, 1).Title != "maintenance" {
					t.Fatalf("%d announcements left, want only the draft deleted", n)
				}
			}},
		{"detail", http.MethodGet, "/v1/admin/announcement/detail?id=1", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, supporttest.AnnouncementView(env.ReloadAnnouncement(t, 1)))
			}},
		{"detail missing", http.MethodGet, "/v1/admin/announcement/detail?id=9", "",
			func(t *testing.T, _ *supporttest.Env, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		// The admin list shows drafts too.
		{"list", http.MethodGet, "/v1/admin/announcement/list?page=1&size=10", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetAnnouncementListResponse{Total: 2, List: []dto.Announcement{supporttest.AnnouncementView(env.ReloadAnnouncement(t, 1)), supporttest.AnnouncementView(env.ReloadAnnouncement(t, 2))}})
			}},
		{"list filtered", http.MethodGet, "/v1/admin/announcement/list?page=1&size=10&show=false&popup=false&search=plans", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetAnnouncementListResponse{Total: 1, List: []dto.Announcement{supporttest.AnnouncementView(env.ReloadAnnouncement(t, 2))}})
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := announcementFixture(t)
			tc.check(t, env, supporttest.Serve(t, h, tc.method, tc.target, tc.body))
		})
	}
}

// A request that does not bind or misses a required field is refused as a
// parameter error and leaves the announcements as they were.
func TestAnnouncementHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"create not JSON", http.MethodPost, "/v1/admin/announcement/", `{"title":"price change"`, ""},
		{"create without content", http.MethodPost, "/v1/admin/announcement/", `{"title":"price change"}`, "Content is a required field"},
		{"update not JSON", http.MethodPut, "/v1/admin/announcement/", `{"id":1,}`, ""},
		{"update without id", http.MethodPut, "/v1/admin/announcement/", `{"title":"renamed","show":false}`, "Id is a required field"},
		{"delete id not a number", http.MethodDelete, "/v1/admin/announcement/", `{"id":"1"}`, ""},
		{"delete without id", http.MethodDelete, "/v1/admin/announcement/", `{}`, "Id is a required field"},
		{"detail id not a number", http.MethodGet, "/v1/admin/announcement/detail?id=first", "", "bind Id"},
		{"detail without id", http.MethodGet, "/v1/admin/announcement/detail", "", "Id is a required field"},
		{"list show not a switch", http.MethodGet, "/v1/admin/announcement/list?page=1&size=10&show=maybe", "", "bind Show"},
		{"list without size", http.MethodGet, "/v1/admin/announcement/list?page=1", "", "Size is a required field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := announcementFixture(t)
			before := env.ReloadAnnouncement(t, 1)
			supporttest.Serve(t, h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			after := env.ReloadAnnouncement(t, 1)
			if env.Count(t, &announcementEntity.Announcement{}) != 2 || after.Title != before.Title || flags(after) != flags(before) {
				t.Fatalf("announcement = %+v, want the announcements unchanged", after)
			}
		})
	}
}
