package document

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	docEntity "github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The user document handlers run against the real support facade over the
// harness database: a published guide with subscription-gated blocks (1), a
// hidden page (2) and a published FAQ (3). User 11 holds an active
// subscription, user 12 does not.

const gated = "install {{#if_subscribed}}the profile{{/if_subscribed}}{{#if_not_subscribed}}a plan first{{/if_not_subscribed}}"

const (
	subscriber = 11
	visitor    = 12
)

// libraryFixture serves the routes to the signed-in user userID, or
// anonymously when userID is 0.
func libraryFixture(t *testing.T, userID int64, subscriptions *supporttest.Subscriptions) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	env.Document(t, docEntity.Document{Title: "iOS guide", Content: gated, Tags: "setup,ios"})
	env.Document(t, docEntity.Document{Title: "internal notes", Content: "runbook", Tags: "ops", Show: new(false)})
	env.Document(t, docEntity.Document{Title: "FAQ", Content: "questions"})
	svc := support.New(support.Deps{Documents: env.Documents, Subscriptions: subscriptions})
	h := server.New()
	if userID != 0 {
		h.Use(func(ctx context.Context, c *app.RequestContext) { c.Next(supporttest.WithUser(ctx, userID)) })
	}
	group := h.Group("/v1/public/document")
	group.GET("/detail", QueryDocumentDetailHandler(svc))
	group.GET("/list", QueryDocumentListHandler(svc))
	return env, h
}

// The gated blocks are rendered for the reader: subscribers see the
// subscribed block, everyone else the other one, and a failed subscription
// check fails closed.
func TestDocumentDetailRendersTheGatedBlocksForTheReader(t *testing.T) {
	down := &supporttest.Subscriptions{Active: map[int64]bool{subscriber: true}, Err: errors.New("subscription store down")}
	for _, tc := range []struct {
		name          string
		user          int64
		subscriptions *supporttest.Subscriptions
		content       string
	}{
		{"subscriber", subscriber, supporttest.Subscribed(subscriber), "install the profile"},
		{"without subscription", visitor, supporttest.Subscribed(subscriber), "install a plan first"},
		{"anonymous", 0, supporttest.Subscribed(subscriber), "install a plan first"},
		{"subscription check failed", subscriber, down, "install a plan first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := libraryFixture(t, tc.user, tc.subscriptions)
			want := supporttest.DocumentView(env.ReloadDocument(t, 1), "setup", "ios")
			want.Content = tc.content
			supporttest.Serve(t, h, http.MethodGet, "/v1/public/document/detail?id=1", "").OK(t, want)
		})
	}
}

// A hidden document answers exactly like a missing one, so the detail can
// neither read nor probe unpublished pages.
func TestDocumentDetailHidesUnpublishedDocuments(t *testing.T) {
	for _, target := range []string{"/v1/public/document/detail?id=2", "/v1/public/document/detail?id=9"} {
		t.Run(target, func(t *testing.T) {
			_, h := libraryFixture(t, subscriber, supporttest.Subscribed(subscriber))
			supporttest.Serve(t, h, http.MethodGet, target, "").Refused(t, xerr.DatabaseQueryError, "Database query error")
		})
	}
}

func TestDocumentDetailRefusesMalformedQueries(t *testing.T) {
	for _, tc := range []struct{ name, target, msg string }{
		{"id not a number", "/v1/public/document/detail?id=guide", "bind Id"},
		{"without id", "/v1/public/document/detail", "Id is a required field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h := libraryFixture(t, subscriber, supporttest.Subscribed(subscriber))
			supporttest.Serve(t, h, http.MethodGet, tc.target, "").Refused(t, xerr.InvalidParams, tc.msg)
		})
	}
}

// The user list names the published documents with their tags, without
// their content.
func TestDocumentListNamesThePublishedDocuments(t *testing.T) {
	env, h := libraryFixture(t, visitor, supporttest.Subscribed(subscriber))
	listed := func(id int64, tags ...string) dto.Document {
		row := env.ReloadDocument(t, id)
		return dto.Document{Id: row.Id, Title: row.Title, Tags: tags, UpdatedAt: row.UpdatedAt.UnixMilli()}
	}
	// A document without tags lists an empty tag list, not null.
	supporttest.Serve(t, h, http.MethodGet, "/v1/public/document/list", "").OK(t, dto.QueryDocumentListResponse{
		Total: 2, List: []dto.Document{listed(1, "setup", "ios"), listed(3, []string{}...)},
	})
}

func TestDocumentListAnswersTheStoreError(t *testing.T) {
	env, h := libraryFixture(t, visitor, supporttest.Subscribed(subscriber))
	sqlDB, err := env.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	supporttest.Serve(t, h, http.MethodGet, "/v1/public/document/list", "").Refused(t, xerr.DatabaseQueryError, "Database query error")
}
