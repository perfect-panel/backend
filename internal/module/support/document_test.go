package support_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/internal/module/support"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	docEntity "github.com/perfect-panel/backend/internal/module/support/entity/document"
	"gorm.io/gorm"
)

type fakeDocumentRepo struct {
	findOne    *docEntity.Document
	findErr    error
	deletedIDs []int64
	deleteErr  error
}

func (f *fakeDocumentRepo) Insert(_ context.Context, _ *docEntity.Document) error { return nil }

func (f *fakeDocumentRepo) FindOne(_ context.Context, _ int64) (*docEntity.Document, error) {
	return f.findOne, f.findErr
}

func (f *fakeDocumentRepo) Update(_ context.Context, _ *docEntity.Document) error { return nil }

func (f *fakeDocumentRepo) Delete(_ context.Context, id int64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, id)
	return nil
}

func (f *fakeDocumentRepo) QueryDocumentDetail(_ context.Context, _ int64) (*docEntity.Document, error) {
	return f.findOne, nil
}

func (f *fakeDocumentRepo) QueryDocumentList(_ context.Context, _, _ int, _, _ string) (int64, []*docEntity.Document, error) {
	return 0, nil, nil
}

func (f *fakeDocumentRepo) GetDocumentListByAll(_ context.Context) (int64, []*docEntity.Document, error) {
	return 0, nil, nil
}

type fakeSubscriptionReader struct {
	active bool
	err    error
}

func (f fakeSubscriptionReader) HasActiveSubscription(_ context.Context, _ int64) (bool, error) {
	return f.active, f.err
}

const gatedContent = "public {{#if_subscribed}}secret{{/if_subscribed}}{{#if_not_subscribed}}subscribe first{{/if_not_subscribed}}"

func newDocService(repo *fakeDocumentRepo, subs support.SubscriptionReader) support.Service {
	return support.New(support.Deps{Documents: repo, Subscriptions: subs})
}

func ctxWithUser(id int64) context.Context {
	return context.WithValue(context.Background(), requestctx.CtxKeyUser, &user.User{Id: id})
}

func queryGated(t *testing.T, svc support.Service, ctx context.Context) string {
	t.Helper()
	resp, err := svc.QueryDocumentDetail(ctx, &dto.QueryDocumentDetailRequest{Id: 1})
	if err != nil {
		t.Fatalf("QueryDocumentDetail: %v", err)
	}
	return resp.Content
}

func TestQueryDocumentDetailShowsGatedContentToSubscribers(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Content: gatedContent, Show: ptr(true)}}
	svc := newDocService(repo, fakeSubscriptionReader{active: true})

	content := queryGated(t, svc, ctxWithUser(9))
	if !strings.Contains(content, "secret") || strings.Contains(content, "subscribe first") {
		t.Fatalf("subscriber must see gated content only: %q", content)
	}
}

func TestQueryDocumentDetailHidesGatedContentWithoutSubscription(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Content: gatedContent, Show: ptr(true)}}
	svc := newDocService(repo, fakeSubscriptionReader{active: false})

	content := queryGated(t, svc, ctxWithUser(9))
	if strings.Contains(content, "secret") || !strings.Contains(content, "subscribe first") {
		t.Fatalf("non-subscriber must not see gated content: %q", content)
	}
}

func TestQueryDocumentDetailHidesGatedContentForAnonymousUser(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Content: gatedContent, Show: ptr(true)}}
	svc := newDocService(repo, fakeSubscriptionReader{active: true})

	content := queryGated(t, svc, context.Background())
	if strings.Contains(content, "secret") {
		t.Fatalf("anonymous user must not see gated content: %q", content)
	}
}

func TestQueryDocumentDetailTreatsPortErrorAsNoSubscription(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Content: gatedContent, Show: ptr(true)}}
	svc := newDocService(repo, fakeSubscriptionReader{active: true, err: errors.New("boom")})

	content := queryGated(t, svc, ctxWithUser(9))
	if strings.Contains(content, "secret") {
		t.Fatalf("port failure must fail closed: %q", content)
	}
}

// The public detail must not serve documents the administrator hid, and a
// hidden document must answer exactly like a missing one.
func TestQueryDocumentDetailTreatsHiddenDocumentAsMissing(t *testing.T) {
	missing := newDocService(&fakeDocumentRepo{findErr: gorm.ErrRecordNotFound}, nil)
	_, missingErr := missing.QueryDocumentDetail(ctxWithUser(9), &dto.QueryDocumentDetailRequest{Id: 1})
	if missingErr == nil {
		t.Fatal("missing document returned no error")
	}

	for name, show := range map[string]*bool{"hidden": ptr(false), "show unset": nil} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Title: "internal", Content: "secret", Show: show}}
			svc := newDocService(repo, fakeSubscriptionReader{active: true})

			resp, err := svc.QueryDocumentDetail(ctxWithUser(9), &dto.QueryDocumentDetailRequest{Id: 1})
			if err == nil || resp != nil {
				t.Fatalf("hidden document served: resp=%+v err=%v", resp, err)
			}
			if err.Error() != missingErr.Error() {
				t.Fatalf("hidden error = %q, want the missing-document error %q", err, missingErr)
			}
		})
	}
}

func TestGetDocumentDetailStillReturnsHiddenDocumentToAdmin(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Title: "internal", Content: "draft", Show: ptr(false)}}
	svc := newDocService(repo, nil)

	resp, err := svc.GetDocumentDetail(context.Background(), &dto.GetDocumentDetailRequest{Id: 1})
	if err != nil {
		t.Fatalf("GetDocumentDetail: %v", err)
	}
	if resp.Id != 1 || resp.Content != "draft" {
		t.Fatalf("admin detail = %+v, want the hidden document", resp)
	}
}

func TestBatchDeleteDocumentDeletesEachID(t *testing.T) {
	repo := &fakeDocumentRepo{}
	svc := newDocService(repo, nil)

	err := svc.BatchDeleteDocument(context.Background(), &dto.BatchDeleteDocumentRequest{Ids: []int64{1, 2, 3}})
	if err != nil {
		t.Fatalf("BatchDeleteDocument: %v", err)
	}
	if len(repo.deletedIDs) != 3 {
		t.Fatalf("deleted %v, want 3 ids", repo.deletedIDs)
	}
}

// Every document view splits the stored comma-separated tags and shows the
// document's own switch: the public detail used to answer tags: null, and
// the admin detail always said hidden.
func TestDocumentDetailsCarryTheirTagsAndSwitch(t *testing.T) {
	repo := &fakeDocumentRepo{findOne: &docEntity.Document{Id: 1, Title: "guide", Content: "text", Tags: "setup,ios,setup", Show: ptr(true)}}
	svc := newDocService(repo, nil)

	public, err := svc.QueryDocumentDetail(context.Background(), &dto.QueryDocumentDetailRequest{Id: 1})
	if err != nil {
		t.Fatalf("QueryDocumentDetail: %v", err)
	}
	admin, err := svc.GetDocumentDetail(context.Background(), &dto.GetDocumentDetailRequest{Id: 1})
	if err != nil {
		t.Fatalf("GetDocumentDetail: %v", err)
	}
	for name, doc := range map[string]*dto.Document{"public": public, "admin": admin} {
		if len(doc.Tags) != 2 || doc.Tags[0] != "setup" || doc.Tags[1] != "ios" || !doc.Show {
			t.Fatalf("%s detail = %+v, want the tags setup and ios and the document shown", name, doc)
		}
	}
}
