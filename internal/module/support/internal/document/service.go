// Package document implements the document subdomain of the support module.
// Only the module facade (internal/module/support) may reach it.
package document

import (
	"context"
	"regexp"
	"strings"

	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	dto "github.com/perfect-panel/backend/internal/module/support/contract"
	entity "github.com/perfect-panel/backend/internal/module/support/entity/document"
	"github.com/perfect-panel/backend/internal/repository"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/slicesx"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// SubscriptionReader is the support module's port onto the subscription
// domain; the composition root adapts the subscription domain's repository
// to it.
type SubscriptionReader interface {
	HasActiveSubscription(ctx context.Context, userID int64) (bool, error)
}

// Subscription-gated conditional blocks in document content. Stripped
// server-side so gated content (e.g. shared credentials) never reaches users
// without an active subscription.
var (
	reIfSubscribed    = regexp.MustCompile(`(?s)\{\{#if_subscribed\}\}(.*?)\{\{/if_subscribed\}\}`)
	reIfNotSubscribed = regexp.MustCompile(`(?s)\{\{#if_not_subscribed\}\}(.*?)\{\{/if_not_subscribed\}\}`)
)

// Service manages the help documents for the support facade.
type Service struct {
	repo repository.DocumentRepo
	subs SubscriptionReader
}

// NewService builds the document service; subs may be nil, and then every
// reader sees the content for users without a subscription.
func NewService(repo repository.DocumentRepo, subs SubscriptionReader) *Service {
	return &Service{repo: repo, subs: subs}
}

// Create stores a new document; its tags are stored comma-separated.
func (s *Service) Create(ctx context.Context, req *dto.CreateDocumentRequest) error {
	if err := s.repo.Insert(ctx, &entity.Document{
		Title:   req.Title,
		Content: req.Content,
		Tags:    strings.Join(req.Tags, ","),
		Show:    req.Show,
	}); err != nil {
		logger.WithContext(ctx).Errorw("[CreateDocument] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert document error: %v", err.Error())
	}
	return nil
}

// Update overwrites a document's title, content and tags. A request without
// the show switch keeps the stored one, since the column cannot be NULL, and
// the document must exist: saving an unknown id used to insert it.
func (s *Service) Update(ctx context.Context, req *dto.UpdateDocumentRequest) error {
	doc, err := s.repo.FindOne(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[UpdateDocument] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find document %d", req.Id)
	}
	doc.Title = req.Title
	doc.Content = req.Content
	doc.Tags = strings.Join(req.Tags, ",")
	if req.Show != nil {
		doc.Show = req.Show
	}
	if err := s.repo.Update(ctx, doc); err != nil {
		logger.WithContext(ctx).Errorw("[UpdateDocument] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseUpdateError, "failed to update document: %v", err.Error())
	}
	return nil
}

// Delete removes a document.
func (s *Service) Delete(ctx context.Context, req *dto.DeleteDocumentRequest) error {
	if err := s.repo.Delete(ctx, req.Id); err != nil {
		logger.WithContext(ctx).Errorw("[DeleteDocument] Database Error", logger.Field("error", err.Error()))
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "failed to delete document: %v", err.Error())
	}
	return nil
}

// BatchDelete removes the documents one by one, stopping at the first
// failure.
func (s *Service) BatchDelete(ctx context.Context, req *dto.BatchDeleteDocumentRequest) error {
	for _, id := range req.Ids {
		if err := s.repo.Delete(ctx, id); err != nil {
			logger.WithContext(ctx).Errorw("[BatchDeleteDocument] Database Error", logger.Field("error", err.Error()))
			return xerr.Wrapf(err, xerr.DatabaseDeletedError, "failed to delete document: %v", err.Error())
		}
	}
	return nil
}

// GetDetail returns a document for the admin panel, hidden ones included.
func (s *Service) GetDetail(ctx context.Context, req *dto.GetDocumentDetailRequest) (*dto.Document, error) {
	data, err := s.repo.QueryDocumentDetail(ctx, req.Id)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetDocumentDetail] Database Error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QueryDocumentDetail error: %v", err.Error())
	}
	view := documentView(data)
	return &view, nil
}

// documentView is a stored document as the API shows it: its tags split from
// their comma-separated column.
func documentView(d *entity.Document) dto.Document {
	return dto.Document{
		Id:        d.Id,
		Title:     d.Title,
		Tags:      slicesx.StringMergeAndRemoveDuplicates(d.Tags),
		Content:   d.Content,
		Show:      d.Show != nil && *d.Show,
		CreatedAt: d.CreatedAt.UnixMilli(),
		UpdatedAt: d.UpdatedAt.UnixMilli(),
	}
}

// List pages the documents for the admin panel.
func (s *Service) List(ctx context.Context, req *dto.GetDocumentListRequest) (*dto.GetDocumentListResponse, error) {
	total, data, err := s.repo.QueryDocumentList(ctx, int(req.Page), int(req.Size), req.Tag, req.Search)
	if err != nil {
		logger.WithContext(ctx).Errorw("[GetDocumentList] Database Error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QueryDocumentList error: %v", err.Error())
	}
	resp := &dto.GetDocumentListResponse{
		Total: total,
		List:  make([]dto.Document, 0),
	}
	for _, v := range data {
		resp.List = append(resp.List, documentView(v))
	}
	return resp, nil
}

// QueryDetail serves the user-facing detail. A hidden document answers
// exactly like a missing one, so the endpoint cannot be used to read or probe
// unpublished documents; the admin detail (GetDetail) still returns them.
func (s *Service) QueryDetail(ctx context.Context, req *dto.QueryDocumentDetailRequest) (*dto.Document, error) {
	data, err := s.repo.FindOne(ctx, req.Id)
	if err == nil && (data.Show == nil || !*data.Show) {
		err = gorm.ErrRecordNotFound
	}
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryDocumentDetail] FindOne error", logger.Field("id", req.Id), logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "FindOne error: %s", err.Error())
	}
	resp := documentView(data)
	resp.Content = s.renderConditional(ctx, resp.Content)
	return &resp, nil
}

// renderConditional keeps or drops {{#if_subscribed}}...{{/if_subscribed}} and
// {{#if_not_subscribed}}...{{/if_not_subscribed}} blocks based on whether the
// current user has an active subscription. Done here (server-side) so gated
// content is never sent to users who shouldn't see it.
func (s *Service) renderConditional(ctx context.Context, content string) string {
	if content == "" {
		return content
	}

	hasSubscription := false
	if u, ok := ctx.Value(requestctx.CtxKeyUser).(*user.User); ok && u != nil && s.subs != nil {
		active, err := s.subs.HasActiveSubscription(ctx, u.Id)
		if err != nil {
			logger.WithContext(ctx).Errorw("[QueryDocumentDetail] QueryUserSubscribe error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		} else {
			hasSubscription = active
		}
	}

	if hasSubscription {
		content = reIfSubscribed.ReplaceAllString(content, "$1")
		content = reIfNotSubscribed.ReplaceAllString(content, "")
	} else {
		content = reIfSubscribed.ReplaceAllString(content, "")
		content = reIfNotSubscribed.ReplaceAllString(content, "$1")
	}
	return content
}

// QueryList lists the shown documents for users, without their content.
func (s *Service) QueryList(ctx context.Context) (*dto.QueryDocumentListResponse, error) {
	total, data, err := s.repo.GetDocumentListByAll(ctx)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryDocumentList] error", logger.Field("error", err.Error()))
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "QueryDocumentList error: %v", err.Error())
	}
	resp := &dto.QueryDocumentListResponse{
		Total: total,
		List:  make([]dto.Document, 0),
	}
	for _, item := range data {
		resp.List = append(resp.List, dto.Document{
			Id:        item.Id,
			Title:     item.Title,
			Tags:      slicesx.StringMergeAndRemoveDuplicates(item.Tags),
			UpdatedAt: item.UpdatedAt.UnixMilli(),
		})
	}
	return resp, nil
}
