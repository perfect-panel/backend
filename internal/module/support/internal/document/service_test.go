package document

import (
	"context"
	"errors"
	"reflect"
	"testing"

	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/module/support/internal/supporttest"
	"github.com/perfect-panel/backend/pkg/logger/logtest"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// The document service runs over the harness repository, with the
// subscription port answering that user 11 subscribes.

var _ SubscriptionReader = (*supporttest.Subscriptions)(nil)

const subscriber, visitor = 11, 12

func newLibrary(t *testing.T, subs SubscriptionReader) (*supporttest.Env, *Service) {
	t.Helper()
	logtest.Discard(t)
	env := supporttest.New(t)
	return env, NewService(env.Documents, subs)
}

// render stores content as a published document and returns it as the
// reader of ctx sees it.
func render(t *testing.T, subs SubscriptionReader, ctx context.Context, content string) string {
	t.Helper()
	env, svc := newLibrary(t, subs)
	doc := env.Document(t, entity.Document{Title: "guide", Content: content})
	resp, err := svc.QueryDetail(ctx, &dto.QueryDocumentDetailRequest{Id: doc.Id})
	if err != nil {
		t.Fatal(err)
	}
	return resp.Content
}

// The gated blocks render for the reader: a subscriber keeps the contents
// of the subscribed blocks and loses the others, everyone else the reverse.
// Text outside the blocks is kept, blocks may span lines and repeat.
func TestConditionalBlocksRenderForTheReader(t *testing.T) {
	const content = "Step 1: install the app.\n" +
		"{{#if_subscribed}}Step 2: import\nyour profile.{{/if_subscribed}}" +
		"{{#if_not_subscribed}}Step 2: buy a plan.{{/if_not_subscribed}}\n" +
		"Server: {{#if_subscribed}}tokyo.example{{/if_subscribed}}{{#if_not_subscribed}}hidden{{/if_not_subscribed}}."
	const subscribed = "Step 1: install the app.\nStep 2: import\nyour profile.\nServer: tokyo.example."
	const other = "Step 1: install the app.\nStep 2: buy a plan.\nServer: hidden."
	failing := &supporttest.Subscriptions{Active: map[int64]bool{subscriber: true}, Err: errors.New("subscription store down")}
	for _, tc := range []struct {
		name string
		subs SubscriptionReader
		ctx  context.Context
		want string
	}{
		{"subscriber", supporttest.Subscribed(subscriber), supporttest.WithUser(context.Background(), subscriber), subscribed},
		{"without subscription", supporttest.Subscribed(subscriber), supporttest.WithUser(context.Background(), visitor), other},
		{"anonymous", supporttest.Subscribed(subscriber), context.Background(), other},
		// Gated content never leaks: a failed check reads as no
		// subscription, and so does a library without the port.
		{"subscription check failed", failing, supporttest.WithUser(context.Background(), subscriber), other},
		{"no subscription port", nil, supporttest.WithUser(context.Background(), subscriber), other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(t, tc.subs, tc.ctx, content); got != tc.want {
				t.Fatalf("content = %q, want %q", got, tc.want)
			}
		})
	}
}

// The subscription is asked for the reader only, and not at all for a
// document without content.
func TestConditionalRenderingAsksForTheReader(t *testing.T) {
	subs := supporttest.Subscribed(subscriber)
	if got := render(t, subs, supporttest.WithUser(context.Background(), visitor), "{{#if_subscribed}}x{{/if_subscribed}}"); got != "" {
		t.Fatalf("content = %q, want the gated block removed", got)
	}
	if !reflect.DeepEqual(subs.Asked, []int64{visitor}) {
		t.Fatalf("asked about %v, want the reader %d", subs.Asked, visitor)
	}
	subs.Asked = nil
	if got := render(t, subs, supporttest.WithUser(context.Background(), visitor), ""); got != "" || subs.Asked != nil {
		t.Fatalf("content = %q, asked about %v, want an empty document and no question", got, subs.Asked)
	}
}

// seedLibrary stores the documents the list tests read: 1 to 3 published,
// 4 hidden.
func seedLibrary(t *testing.T, env *supporttest.Env) {
	t.Helper()
	for _, row := range []entity.Document{
		{Title: "iOS guide", Content: "open the app store", Tags: "setup,ios,setup"},
		{Title: "iOS beta", Content: "join the beta", Tags: "ios-beta"},
		{Title: "Android guide", Content: "open the play store", Tags: "android,setup"},
		{Title: "runbook", Content: "restart the store", Tags: "ops", Show: new(false)},
	} {
		env.Document(t, row)
	}
}

func ids(list []dto.Document) []int64 {
	out := []int64{}
	for _, item := range list {
		out = append(out, item.Id)
	}
	return out
}

// The admin list shows hidden documents too, narrows to one exact tag and to
// a search of title and content, and pages with the total of every match.
func TestAdminListFiltersAndPages(t *testing.T) {
	env, svc := newLibrary(t, nil)
	seedLibrary(t, env)
	for _, tc := range []struct {
		name  string
		req   dto.GetDocumentListRequest
		total int64
		ids   []int64
	}{
		{"all", dto.GetDocumentListRequest{Page: 1, Size: 10}, 4, []int64{1, 2, 3, 4}},
		// A tag matches whole tags only: ios is not ios-beta.
		{"tag", dto.GetDocumentListRequest{Page: 1, Size: 10, Tag: "ios"}, 1, []int64{1}},
		{"shared tag", dto.GetDocumentListRequest{Page: 1, Size: 10, Tag: "setup"}, 2, []int64{1, 3}},
		{"search", dto.GetDocumentListRequest{Page: 1, Size: 10, Search: "store"}, 3, []int64{1, 3, 4}},
		{"tag and search", dto.GetDocumentListRequest{Page: 1, Size: 10, Tag: "setup", Search: "play"}, 1, []int64{3}},
		{"second page", dto.GetDocumentListRequest{Page: 2, Size: 3}, 4, []int64{4}},
		{"no match", dto.GetDocumentListRequest{Page: 1, Size: 10, Tag: "windows"}, 0, []int64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.List(context.Background(), &tc.req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.Total != tc.total || resp.List == nil || !reflect.DeepEqual(ids(resp.List), tc.ids) {
				t.Fatalf("list = %d %v, want %d %v", resp.Total, ids(resp.List), tc.total, tc.ids)
			}
		})
	}
	resp, err := svc.List(context.Background(), &dto.GetDocumentListRequest{Page: 1, Size: 1, Tag: "ios"})
	if err != nil || !reflect.DeepEqual(resp.List, []dto.Document{supporttest.DocumentView(env.ReloadDocument(t, 1), "setup", "ios")}) {
		t.Fatalf("list = %+v (err %v), want the guide with its tags once each", resp, err)
	}
}

// The user list names the published documents, their tags split, without
// their content.
func TestUserListNamesThePublishedDocuments(t *testing.T) {
	env, svc := newLibrary(t, nil)
	seedLibrary(t, env)
	resp, err := svc.QueryList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := &dto.QueryDocumentListResponse{Total: 3}
	for id, tags := range map[int64][]string{1: {"setup", "ios"}, 2: {"ios-beta"}, 3: {"android", "setup"}} {
		row := env.ReloadDocument(t, id)
		want.List = append(want.List, dto.Document{Id: id, Title: row.Title, Tags: tags, UpdatedAt: row.UpdatedAt.UnixMilli()})
	}
	byID := func(list []dto.Document) map[int64]dto.Document {
		out := map[int64]dto.Document{}
		for _, item := range list {
			out[item.Id] = item
		}
		return out
	}
	if resp.Total != 3 || !reflect.DeepEqual(byID(resp.List), byID(want.List)) {
		t.Fatalf("list = %+v, want %+v", resp, want)
	}
}

// The user detail answers a hidden document exactly like a missing one; the
// admin detail shows it, and reports a missing one as not found.
func TestHiddenDocumentsAreAdminOnly(t *testing.T) {
	env, svc := newLibrary(t, supporttest.Subscribed(subscriber))
	seedLibrary(t, env)
	reader := supporttest.WithUser(context.Background(), subscriber)
	for _, id := range []int64{4, 9} {
		if _, err := svc.QueryDetail(reader, &dto.QueryDocumentDetailRequest{Id: id}); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("user detail of %d: %v, want not found", id, err)
		}
	}
	detail, err := svc.GetDetail(context.Background(), &dto.GetDocumentDetailRequest{Id: 4})
	if err != nil || !reflect.DeepEqual(*detail, supporttest.DocumentView(env.ReloadDocument(t, 4), "ops")) {
		t.Fatalf("admin detail = %+v (err %v), want the hidden document", detail, err)
	}
	if _, err := svc.GetDetail(context.Background(), &dto.GetDocumentDetailRequest{Id: 9}); xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("admin detail of a missing document: %v, want not found", err)
	}
}

// Tags are stored comma-separated; a document created without its switch is
// published, an update replaces every field, and deletes remove documents
// one by one.
func TestDocumentsAreWrittenAsSent(t *testing.T) {
	env, svc := newLibrary(t, nil)
	ctx := context.Background()
	if err := svc.Create(ctx, &dto.CreateDocumentRequest{Title: "FAQ", Content: "questions", Tags: []string{"help", "faq"}}); err != nil {
		t.Fatal(err)
	}
	if got := env.ReloadDocument(t, 1); got.Tags != "help,faq" || !*got.Show {
		t.Fatalf("created = %+v, want the tags joined and the document published", got)
	}
	if err := svc.Update(ctx, &dto.UpdateDocumentRequest{Id: 1, Title: "FAQ v2", Content: "answers", Tags: []string{"help"}, Show: new(false)}); err != nil {
		t.Fatal(err)
	}
	if got := env.ReloadDocument(t, 1); got.Title != "FAQ v2" || got.Content != "answers" || got.Tags != "help" || *got.Show {
		t.Fatalf("updated = %+v, want every field replaced", got)
	}
	seedLibrary(t, env)
	if err := svc.Delete(ctx, &dto.DeleteDocumentRequest{Id: 1}); err != nil {
		t.Fatal(err)
	}
	// A batch skips the documents already gone.
	if err := svc.BatchDelete(ctx, &dto.BatchDeleteDocumentRequest{Ids: []int64{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	var left []int64
	if err := env.DB.Model(&entity.Document{}).Order("id").Pluck("id", &left).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, []int64{4, 5}) {
		t.Fatalf("documents left = %v, want 4 and 5", left)
	}
}

// An update without the show switch keeps the stored one (writing NULL
// failed on the NOT NULL column), keeps the creation time, and an unknown id
// is reported instead of being inserted.
func TestUpdateKeepsWhatTheRequestOmits(t *testing.T) {
	env, svc := newLibrary(t, nil)
	ctx := context.Background()
	hidden := env.Document(t, entity.Document{Title: "draft", Content: "wip", Show: new(false)})
	created := env.ReloadDocument(t, hidden.Id).CreatedAt

	if err := svc.Update(ctx, &dto.UpdateDocumentRequest{Id: hidden.Id, Title: "draft v2", Content: "done"}); err != nil {
		t.Fatalf("Update without show: %v", err)
	}
	got := env.ReloadDocument(t, hidden.Id)
	if got.Title != "draft v2" || got.Content != "done" || got.Show == nil || *got.Show {
		t.Fatalf("updated = %+v, want the new text and the document still hidden", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %v, want %v kept", got.CreatedAt, created)
	}

	before := env.Count(t, &entity.Document{})
	err := svc.Update(ctx, &dto.UpdateDocumentRequest{Id: 999, Title: "ghost", Content: "c", Show: new(true)})
	if xerr.CodeOf(err) != xerr.DatabaseQueryError || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("Update of an unknown id = %v, want not found", err)
	}
	if env.Count(t, &entity.Document{}) != before {
		t.Fatal("the update of an unknown id inserted a document")
	}
}

// A batch deletes the documents in order and stops at the first one the
// store refuses, leaving the rest.
func TestBatchDeleteStopsAtTheFirstFailure(t *testing.T) {
	env, svc := newLibrary(t, nil)
	seedLibrary(t, env)
	refused := errors.New("disk full")
	env.Refuse(t, "delete", "document", 1, refused)

	err := svc.BatchDelete(context.Background(), &dto.BatchDeleteDocumentRequest{Ids: []int64{1, 2, 3}})
	if xerr.CodeOf(err) != xerr.DatabaseDeletedError || !errors.Is(err, refused) {
		t.Fatalf("error = %v, want the refused delete", err)
	}
	var left []int64
	if err := env.DB.Model(&entity.Document{}).Order("id").Pluck("id", &left).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, []int64{2, 3, 4}) {
		t.Fatalf("documents left = %v, want only the first deleted", left)
	}
}

// A statement the store refuses is reported under the failing operation's
// code, and the documents stay as they were.
func TestDocumentStoreFailures(t *testing.T) {
	refused := errors.New("disk full")
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		op   string
		run  func(s *Service) error
		code uint32
	}{
		{"create", "create", func(s *Service) error {
			return s.Create(ctx, &dto.CreateDocumentRequest{Title: "t", Content: "c"})
		}, xerr.DatabaseInsertError},
		{"update", "update", func(s *Service) error {
			return s.Update(ctx, &dto.UpdateDocumentRequest{Id: 1, Title: "t", Content: "c", Show: new(true)})
		}, xerr.DatabaseUpdateError},
		{"delete", "delete", func(s *Service) error { return s.Delete(ctx, &dto.DeleteDocumentRequest{Id: 1}) }, xerr.DatabaseDeletedError},
		{"admin detail", "query", func(s *Service) error {
			_, err := s.GetDetail(ctx, &dto.GetDocumentDetailRequest{Id: 1})
			return err
		}, xerr.DatabaseQueryError},
		{"admin list", "query", func(s *Service) error {
			_, err := s.List(ctx, &dto.GetDocumentListRequest{Page: 1, Size: 10})
			return err
		}, xerr.DatabaseQueryError},
		{"user list", "query", func(s *Service) error {
			_, err := s.QueryList(ctx)
			return err
		}, xerr.DatabaseQueryError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, svc := newLibrary(t, nil)
			seedLibrary(t, env)
			lift := env.Refuse(t, tc.op, "document", 0, refused)
			err := tc.run(svc)
			lift()
			if xerr.CodeOf(err) != tc.code || !errors.Is(err, refused) {
				t.Fatalf("error = %v, want code %d wrapping the refusal", err, tc.code)
			}
			if got := env.ReloadDocument(t, 1); got.Title != "iOS guide" || env.Count(t, &entity.Document{}) != 4 {
				t.Fatalf("document = %+v, want the library unchanged", got)
			}
		})
	}
}
