package ads

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	adsEntity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The ads handlers run against the real support facade over the harness
// database: each case starts from one stored ad, "spring sale" (id 1).

var (
	springStart = time.UnixMilli(1767225600000) // 2026-01-01T00:00:00Z
	springEnd   = time.UnixMilli(1769904000000) // 2026-02-01T00:00:00Z
)

func adsFixture(t *testing.T) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	env.Ad(t, adsEntity.Ads{Title: "spring sale", Type: "image", Content: "https://cdn.example/spring.png", Description: "20% off",
		TargetURL: "https://panel.example/buy", StartTime: springStart, EndTime: springEnd, Status: 1})
	svc := support.New(support.Deps{Ads: env.Ads})
	h := server.New()
	group := h.Group("/v1/admin/ads")
	group.POST("/", CreateAdsHandler(svc))
	group.PUT("/", UpdateAdsHandler(svc))
	group.DELETE("/", DeleteAdsHandler(svc))
	group.GET("/detail", GetAdsDetailHandler(svc))
	group.GET("/list", GetAdsListHandler(svc))
	return env, h
}

func ms(at time.Time) string { return strconv.FormatInt(at.UnixMilli(), 10) }

func TestAdsHandlersServeTheStoredAds(t *testing.T) {
	summerStart, summerEnd := springStart.AddDate(0, 6, 0), springEnd.AddDate(0, 6, 0)
	for _, tc := range []struct {
		name, method, target, body string
		check                      func(t *testing.T, env *supporttest.Env, reply supporttest.Reply)
	}{
		{"create", http.MethodPost, "/v1/admin/ads/",
			`{"title":"summer sale","type":"video","content":"https://cdn.example/summer.mp4","description":"30% off",` +
				`"target_url":"https://panel.example/summer","start_time":` + ms(summerStart) + `,"end_time":` + ms(summerEnd) + `,"status":0}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadAd(t, 2)
				if got.Title != "summer sale" || got.Type != "video" || got.Content != "https://cdn.example/summer.mp4" || got.Description != "30% off" ||
					got.TargetURL != "https://panel.example/summer" || !got.StartTime.Equal(summerStart) || !got.EndTime.Equal(summerEnd) || got.Status != 0 {
					t.Fatalf("stored ad = %+v, want the request's", got)
				}
			}},
		{"update", http.MethodPut, "/v1/admin/ads/",
			`{"id":1,"title":"spring sale ends","type":"image","content":"https://cdn.example/last.png","description":"last days",` +
				`"target_url":"https://panel.example/buy","start_time":` + ms(springStart) + `,"end_time":` + ms(summerEnd) + `,"status":0}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadAd(t, 1)
				if got.Title != "spring sale ends" || got.Content != "https://cdn.example/last.png" || got.Description != "last days" ||
					!got.EndTime.Equal(summerEnd) || got.Status != 0 {
					t.Fatalf("stored ad = %+v, want the update applied", got)
				}
			}},
		{"delete", http.MethodDelete, "/v1/admin/ads/", `{"id":1}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if n := env.Count(t, &adsEntity.Ads{}); n != 0 {
					t.Fatalf("%d ads left, want the ad deleted", n)
				}
			}},
		// Deleting an ad that is already gone succeeds: the admin panel may
		// send the delete twice.
		{"delete missing", http.MethodDelete, "/v1/admin/ads/", `{"id":9}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if n := env.Count(t, &adsEntity.Ads{}); n != 1 {
					t.Fatalf("%d ads left, want the stored ad kept", n)
				}
			}},
		{"detail", http.MethodGet, "/v1/admin/ads/detail?id=1", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, supporttest.AdView(env.ReloadAd(t, 1)))
			}},
		{"detail missing", http.MethodGet, "/v1/admin/ads/detail?id=9", "",
			func(t *testing.T, _ *supporttest.Env, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		{"list", http.MethodGet, "/v1/admin/ads/list?page=1&size=10&status=1&search=spring", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetAdsListResponse{Total: 1, List: []dto.Ads{supporttest.AdView(env.ReloadAd(t, 1))}})
			}},
		// No ad matches: the list is empty, not null.
		{"list without match", http.MethodGet, "/v1/admin/ads/list?page=1&size=10&status=0", "",
			func(t *testing.T, _ *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, `{"total":0,"list":[]}`)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := adsFixture(t)
			tc.check(t, env, supporttest.Serve(t, h, tc.method, tc.target, tc.body))
		})
	}
}

// A request that does not bind, or asks for an invalid page, is refused as a
// parameter error and leaves the ads as they were.
func TestAdsHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"create not JSON", http.MethodPost, "/v1/admin/ads/", `{"title":`, ""},
		{"create status not a number", http.MethodPost, "/v1/admin/ads/", `{"title":"summer sale","status":"on"}`, ""},
		{"update not JSON", http.MethodPut, "/v1/admin/ads/", `{"id":1,"title"}`, ""},
		{"update id not a number", http.MethodPut, "/v1/admin/ads/", `{"id":"1","title":"renamed"}`, ""},
		{"delete not an object", http.MethodDelete, "/v1/admin/ads/", `[1]`, ""},
		{"detail id not a number", http.MethodGet, "/v1/admin/ads/detail?id=first", "", "bind Id"},
		{"list page not a number", http.MethodGet, "/v1/admin/ads/list?page=one&size=10", "", "bind Page"},
		{"list status not a number", http.MethodGet, "/v1/admin/ads/list?page=1&size=10&status=on", "", "bind Status"},
		{"list without page", http.MethodGet, "/v1/admin/ads/list?size=10", "", "Page is a required field"},
		{"list size too large", http.MethodGet, "/v1/admin/ads/list?page=1&size=101", "", "Size must be 100 or less"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := adsFixture(t)
			before := env.ReloadAd(t, 1)
			supporttest.Serve(t, h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			if after := env.ReloadAd(t, 1); env.Count(t, &adsEntity.Ads{}) != 1 || after.Title != before.Title || after.Status != before.Status {
				t.Fatalf("ad = %+v, want it unchanged", after)
			}
		})
	}
}
