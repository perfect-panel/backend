package user

import (
	"context"

	"github.com/perfect-panel/server/pkg/constant"

	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type QueryUserAffiliateListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Query User Affiliate List
func NewQueryUserAffiliateListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryUserAffiliateListLogic {
	return &QueryUserAffiliateListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QueryUserAffiliateListLogic) QueryUserAffiliateList(req *dto.QueryUserAffiliateListRequest) (resp *dto.QueryUserAffiliateListResponse, err error) {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	data, total, err := l.svcCtx.Store.User().QueryAffiliateList(l.ctx, u.Id, req.Page, req.Size)
	if err != nil {
		l.Errorw("Query User Affiliate List failed: %v", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query User Affiliate List failed: %v", err.Error())
	}

	list := make([]dto.UserAffiliate, 0)
	for _, item := range data {
		list = append(list, dto.UserAffiliate{
			Identifier:   GetAuthMethod(l, item).AuthIdentifier,
			Avatar:       item.Avatar,
			RegisteredAt: item.CreatedAt.UnixMilli(),
			Enable:       *item.Enable,
		})
	}
	return &dto.QueryUserAffiliateListResponse{
		Total: total,
		List:  list,
	}, nil
}

func GetAuthMethod(l *QueryUserAffiliateListLogic, item *user.User) user.AuthMethods {
	authMethod := user.AuthMethods{}
	authMethods := item.AuthMethods
	if len(authMethods) == 0 {
		methods, errs := l.svcCtx.Store.UserAuth().FindUserAuthMethods(l.ctx, item.Id)
		if errs == nil {
			for _, method := range methods {
				authMethods = append(authMethods, *method)
			}
		}
	}
	if len(authMethods) > 0 {
		for _, am := range authMethods {
			if am.AuthType == "6" || am.AuthType == "7" {
				authMethod = am
				break
			}
		}
		if authMethod.AuthIdentifier == "" {
			authMethod = authMethods[0]
		}

		hideTextLength := len(authMethod.AuthIdentifier) / 3
		if hideTextLength > 0 {
			authMethod.AuthIdentifier = authMethod.AuthIdentifier[0:hideTextLength] + "***" + authMethod.AuthIdentifier[hideTextLength*2:]
		}
	}
	return authMethod
}
