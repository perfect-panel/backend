package user

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/phone"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetUserListLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logger.Logger
}

func NewGetUserListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetUserListLogic {
	return &GetUserListLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logger.WithContext(ctx),
	}
}
func (l *GetUserListLogic) GetUserList(req *dto.GetUserListRequest) (*dto.GetUserListResponse, error) {
	list, total, err := l.svcCtx.Store.User().QueryPageList(l.ctx, req.Page, req.Size, &user.UserFilterParams{
		UserId:             req.UserId,
		Search:             req.Search,
		Unscoped:           req.Unscoped,
		SubscribeId:        req.SubscribeId,
		UserSubscribeId:    req.UserSubscribeId,
		UserSubscribeToken: req.UserSubscribeToken,
		Order:              "DESC",
	})
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetUserListLogic failed: %v", err.Error())
	}

	userRespList := make([]dto.User, 0, len(list))

	for _, item := range list {
		var u dto.User
		tool.DeepCopy(&u, item)
		if item.DeletedAt.Valid {
			u.DeletedAt = item.DeletedAt.Time.UnixMilli()
		}

		// 处理 AuthMethods
		authMethods := make([]dto.UserAuthMethod, len(u.AuthMethods)) // 直接创建目标 slice
		for i, method := range u.AuthMethods {
			tool.DeepCopy(&authMethods[i], method)
			if method.AuthType == "mobile" {
				authMethods[i].AuthIdentifier = phone.FormatToInternational(method.AuthIdentifier)
			}
		}
		u.AuthMethods = authMethods

		userRespList = append(userRespList, u)
	}

	return &dto.GetUserListResponse{
		Total: total,
		List:  userRespList,
	}, nil
}
