package document

import (
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	docEntity "github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The document handlers run against the real support facade over the
// harness database: each case starts from a published guide, "iOS guide"
// (id 1), and a hidden page, "internal notes" (id 2).

const gated = "install {{#if_subscribed}}the profile{{/if_subscribed}}{{#if_not_subscribed}}a plan first{{/if_not_subscribed}}"

func documentFixture(t *testing.T) (*supporttest.Env, *server.Hertz) {
	t.Helper()
	env := supporttest.New(t)
	env.Document(t, docEntity.Document{Title: "iOS guide", Content: gated, Tags: "setup,ios"})
	env.Document(t, docEntity.Document{Title: "internal notes", Content: "runbook", Tags: "ops", Show: new(false)})
	svc := support.New(support.Deps{Documents: env.Documents})
	h := server.New()
	group := h.Group("/v1/admin/document")
	group.POST("/", CreateDocumentHandler(svc))
	group.PUT("/", UpdateDocumentHandler(svc))
	group.DELETE("/", DeleteDocumentHandler(svc))
	group.DELETE("/batch", BatchDeleteDocumentHandler(svc))
	group.GET("/detail", GetDocumentDetailHandler(svc))
	group.GET("/list", GetDocumentListHandler(svc))
	return env, h
}

func TestDocumentHandlersServeTheStoredDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		check                      func(t *testing.T, env *supporttest.Env, reply supporttest.Reply)
	}{
		{"create", http.MethodPost, "/v1/admin/document/", `{"title":"Android guide","content":"open the app","tags":["setup","android"],"show":false}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadDocument(t, 3)
				if got.Title != "Android guide" || got.Content != "open the app" || got.Tags != "setup,android" || *got.Show {
					t.Fatalf("stored document = %+v, want the request's, hidden", got)
				}
			}},
		// A document created without its switch is published.
		{"create without switch", http.MethodPost, "/v1/admin/document/", `{"title":"FAQ","content":"questions"}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if got := env.ReloadDocument(t, 3); got.Title != "FAQ" || got.Tags != "" || !*got.Show {
					t.Fatalf("stored document = %+v, want it shown", got)
				}
			}},
		{"update", http.MethodPut, "/v1/admin/document/", `{"id":2,"title":"release notes","content":"v2 is out","tags":["news"],"show":true}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				got := env.ReloadDocument(t, 2)
				if got.Title != "release notes" || got.Content != "v2 is out" || got.Tags != "news" || !*got.Show {
					t.Fatalf("stored document = %+v, want the update applied", got)
				}
			}},
		{"delete", http.MethodDelete, "/v1/admin/document/", `{"id":1}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if n := env.Count(t, &docEntity.Document{}); n != 1 || env.ReloadDocument(t, 2).Title != "internal notes" {
					t.Fatalf("%d documents left, want only the guide deleted", n)
				}
			}},
		// A batch skips the documents already gone.
		{"batch delete", http.MethodDelete, "/v1/admin/document/batch", `{"ids":[1,9,2]}`,
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, nil)
				if n := env.Count(t, &docEntity.Document{}); n != 0 {
					t.Fatalf("%d documents left, want both deleted", n)
				}
			}},
		// The admin detail shows hidden documents, and gated blocks as
		// written.
		{"detail of a hidden document", http.MethodGet, "/v1/admin/document/detail?id=2", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, supporttest.DocumentView(env.ReloadDocument(t, 2), "ops"))
			}},
		{"detail with gated blocks", http.MethodGet, "/v1/admin/document/detail?id=1", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, supporttest.DocumentView(env.ReloadDocument(t, 1), "setup", "ios"))
			}},
		{"detail missing", http.MethodGet, "/v1/admin/document/detail?id=9", "",
			func(t *testing.T, _ *supporttest.Env, reply supporttest.Reply) {
				reply.Refused(t, xerr.DatabaseQueryError, "Database query error")
			}},
		{"list", http.MethodGet, "/v1/admin/document/list?page=1&size=10", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetDocumentListResponse{Total: 2, List: []dto.Document{
					supporttest.DocumentView(env.ReloadDocument(t, 1), "setup", "ios"), supporttest.DocumentView(env.ReloadDocument(t, 2), "ops"),
				}})
			}},
		{"list by tag", http.MethodGet, "/v1/admin/document/list?page=1&size=10&tag=ios", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetDocumentListResponse{Total: 1, List: []dto.Document{supporttest.DocumentView(env.ReloadDocument(t, 1), "setup", "ios")}})
			}},
		{"list by search", http.MethodGet, "/v1/admin/document/list?page=1&size=10&search=runbook", "",
			func(t *testing.T, env *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, dto.GetDocumentListResponse{Total: 1, List: []dto.Document{supporttest.DocumentView(env.ReloadDocument(t, 2), "ops")}})
			}},
		// A page past the end keeps the total and lists nothing.
		{"list past the end", http.MethodGet, "/v1/admin/document/list?page=2&size=10", "",
			func(t *testing.T, _ *supporttest.Env, reply supporttest.Reply) {
				reply.OK(t, `{"total":2,"list":[]}`)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := documentFixture(t)
			tc.check(t, env, supporttest.Serve(t, h, tc.method, tc.target, tc.body))
		})
	}
}

// A request that does not bind or misses a required field is refused as a
// parameter error and leaves the documents as they were.
func TestDocumentHandlersRefuseMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, body string
		msg                        string
	}{
		{"create not JSON", http.MethodPost, "/v1/admin/document/", `{"title":"FAQ","content":`, ""},
		{"create tags not a list", http.MethodPost, "/v1/admin/document/", `{"title":"FAQ","content":"questions","tags":"setup"}`, ""},
		{"create without title", http.MethodPost, "/v1/admin/document/", `{"content":"questions"}`, "Title is a required field"},
		{"update not JSON", http.MethodPut, "/v1/admin/document/", `{"id":1`, ""},
		{"update without content", http.MethodPut, "/v1/admin/document/", `{"id":1,"title":"iOS guide","show":true}`, "Content is a required field"},
		{"delete not an object", http.MethodDelete, "/v1/admin/document/", `"1"`, ""},
		{"delete without id", http.MethodDelete, "/v1/admin/document/", `{}`, "Id is a required field"},
		{"batch delete ids not numbers", http.MethodDelete, "/v1/admin/document/batch", `{"ids":["1","2"]}`, ""},
		{"batch delete without ids", http.MethodDelete, "/v1/admin/document/batch", `{}`, "Ids is a required field"},
		{"detail id not a number", http.MethodGet, "/v1/admin/document/detail?id=guide", "", "bind Id"},
		{"detail without id", http.MethodGet, "/v1/admin/document/detail", "", "Id is a required field"},
		{"list page not a number", http.MethodGet, "/v1/admin/document/list?page=last&size=10", "", "bind Page"},
		{"list size too large", http.MethodGet, "/v1/admin/document/list?page=1&size=1000", "", "Size must be 100 or less"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, h := documentFixture(t)
			before := env.ReloadDocument(t, 1)
			supporttest.Serve(t, h, tc.method, tc.target, tc.body).Refused(t, xerr.InvalidParams, tc.msg)
			after := env.ReloadDocument(t, 1)
			if env.Count(t, &docEntity.Document{}) != 2 || after.Title != before.Title || after.Content != before.Content {
				t.Fatalf("document = %+v, want the documents unchanged", after)
			}
		})
	}
}
