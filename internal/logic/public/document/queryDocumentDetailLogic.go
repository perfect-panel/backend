package document

import (
	"context"
	"regexp"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Subscription-gated conditional blocks in document content. Stripped
// server-side so gated content (e.g. shared credentials) never reaches users
// without an active subscription.
var (
	reIfSubscribed    = regexp.MustCompile(`(?s)\{\{#if_subscribed\}\}(.*?)\{\{/if_subscribed\}\}`)
	reIfNotSubscribed = regexp.MustCompile(`(?s)\{\{#if_not_subscribed\}\}(.*?)\{\{/if_not_subscribed\}\}`)
)

type QueryDocumentDetailLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get document detail
func NewQueryDocumentDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryDocumentDetailLogic {
	return &QueryDocumentDetailLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryDocumentDetailLogic) QueryDocumentDetail(req *dto.QueryDocumentDetailRequest) (resp *dto.Document, err error) {
	// find document
	data, err := l.svcCtx.Store.Document().FindOne(l.ctx, req.Id)
	if err != nil {
		l.Errorw("[QueryDocumentDetailLogic] FindOne error", logger.Field("id", req.Id), logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "FindOne error: %s", err.Error())
	}
	resp = &dto.Document{}
	tool.DeepCopy(resp, data)
	resp.Content = l.renderConditional(resp.Content)
	return
}

// renderConditional keeps or drops {{#if_subscribed}}...{{/if_subscribed}} and
// {{#if_not_subscribed}}...{{/if_not_subscribed}} blocks based on whether the
// current user has an active subscription. Done here (server-side) so gated
// content is never sent to users who shouldn't see it.
func (l *QueryDocumentDetailLogic) renderConditional(content string) string {
	if content == "" {
		return content
	}

	hasSubscription := false
	if u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User); ok && u != nil {
		// status 1 = active
		subs, err := l.svcCtx.Store.UserSubscription().QueryUserSubscribe(l.ctx, u.Id, 1)
		if err != nil {
			l.Errorw("[QueryDocumentDetailLogic] QueryUserSubscribe error", logger.Field("error", err.Error()), logger.Field("user_id", u.Id))
		} else {
			hasSubscription = len(subs) > 0
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
