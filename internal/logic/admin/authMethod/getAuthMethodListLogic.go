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

type GetAuthMethodListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetAuthMethodListLogic Get auth method list
func NewGetAuthMethodListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetAuthMethodListLogic {
	return &GetAuthMethodListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetAuthMethodListLogic) GetAuthMethodList() (resp *dto.GetAuthMethodListResponse, err error) {
	methods, err := l.svcCtx.Store.Auth().FindAll(l.ctx)
	if err != nil {
		l.Errorw("find all failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find all failed: %v", err.Error())
	}
	var list []dto.AuthMethodConfig
	for _, method := range methods {
		var item dto.AuthMethodConfig
		tool.DeepCopy(&item, method)
		if method.Config != "" {
			if err := json.Unmarshal([]byte(method.Config), &item.Config); err != nil {
				l.Errorw("unmarshal config failed", logger.Field("config", method.Config), logger.Field("error", err.Error()))
				return nil, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal config failed: %v", err.Error())
			}
		}
		list = append(list, item)
	}
	return &dto.GetAuthMethodListResponse{List: list}, nil
}
