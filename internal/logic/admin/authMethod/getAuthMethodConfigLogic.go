package authMethod

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetAuthMethodConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetAuthMethodConfigLogic Get auth method config
func NewGetAuthMethodConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthMethodConfigLogic {
	return &GetAuthMethodConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthMethodConfigLogic) GetAuthMethodConfig(req *dto.GetAuthMethodConfigRequest) (resp *dto.AuthMethodConfig, err error) {
	method, err := l.svcCtx.Store.Auth().FindOneByMethod(l.ctx, req.Method)
	if err != nil {
		l.Errorw("find one by method failed", logger.Field("method", req.Method), logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find one by method failed: %v", err.Error())
	}

	resp = new(dto.AuthMethodConfig)
	tool.DeepCopy(resp, method)
	if method.Config != "" {
		if err := json.Unmarshal([]byte(method.Config), &resp.Config); err != nil {
			l.Errorw("unmarshal config failed", logger.Field("config", method.Config), logger.Field("error", err.Error()))
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal apple config failed: %v", err.Error())
		}
	}
	return
}
