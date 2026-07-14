package order

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetOrderListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetOrderListLogic Get order list
func NewGetOrderListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetOrderListLogic {
	return &GetOrderListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetOrderListLogic) GetOrderList(req *dto.GetOrderListRequest) (resp *dto.GetOrderListResponse, err error) {
	total, list, err := l.svcCtx.Store.Order().QueryOrderListByPage(l.ctx, int(req.Page), int(req.Size), req.Status, req.UserId, req.SubscribeId, req.Search)
	if err != nil {
		l.Errorw("[GetOrderList] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QueryOrderListByPage error: %v", err.Error())
	}
	resp = &dto.GetOrderListResponse{}
	resp.List = make([]dto.Order, 0)
	tool.DeepCopy(&resp.List, list)
	resp.Total = total
	return
}
