package common

import (
	"net/http"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	adsEntity "github.com/perfect-panel/backend/internal/module/support/entity/ads"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The public ads run against the real support facade over the harness
// database.

func adsFixture(t *testing.T) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	now := time.Now()
	unset := time.UnixMilli(0) // what the admin API stores for an omitted time
	for _, ad := range []adsEntity.Ads{
		{Title: "running", StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour), Status: 1},
		{Title: "upcoming", StartTime: now.Add(time.Hour), EndTime: now.Add(2 * time.Hour), Status: 1},
		{Title: "expired", StartTime: now.Add(-2 * time.Hour), EndTime: now.Add(-time.Hour), Status: 1},
		{Title: "disabled", StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour), Status: 0},
		{Title: "unscheduled", StartTime: unset, EndTime: unset, Status: 1},
	} {
		env.Ad(t, ad)
	}
	h := server.New()
	h.GET("/v1/common/ads", GetAdsHandler(support.New(support.Deps{Ads: env.Ads})))
	return env, h
}

// The site shows the enabled ads inside their schedule, whatever device or
// position it asks for.
func TestPublicAdsShowTheRunningAds(t *testing.T) {
	for _, query := range []string{"", "?device=ios&position=home"} {
		t.Run(query, func(t *testing.T) {
			env, h := adsFixture(t)
			want := dto.GetAdsResponse{List: []dto.Ads{supporttest.AdView(env.ReloadAd(t, 1)), supporttest.AdView(env.ReloadAd(t, 5))}}
			supporttest.Serve(t, h, http.MethodGet, "/v1/common/ads"+query, "").OK(t, want)
		})
	}
}

// Some clients send a JSON document even with a GET; one that does not
// parse is refused as a parameter error.
func TestPublicAdsRefuseAMalformedBody(t *testing.T) {
	_, h := adsFixture(t)
	supporttest.Serve(t, h, http.MethodGet, "/v1/common/ads", `{"device":`).Refused(t, xerr.InvalidParams, "")
}
