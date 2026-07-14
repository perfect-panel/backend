package user

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateUserRulesLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateUserRulesLogic Update User Rules
func NewUpdateUserRulesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateUserRulesLogic {
	return &UpdateUserRulesLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateUserRulesLogic) UpdateUserRules(req *dto.UpdateUserRulesRequest) error {
	u, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	if len(req.Rules) > 0 {
		bytes, err := json.Marshal(req.Rules)
		if err != nil {
			l.Logger.Errorf("UpdateUserRulesLogic json marshal rules error: %v", err)
			return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "json marshal rules failed: %v", err.Error())
		}
		u.Rules = string(bytes)
		err = l.svcCtx.Store.User().Update(l.ctx, u)
		if err != nil {
			l.Logger.Errorf("UpdateUserRulesLogic UpdateUserRules error: %v", err)
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update user rules failed: %v", err.Error())
		}
	}
	return nil
}
