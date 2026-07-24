// Package userorder implements the user-facing order query subdomain of the
// billing module (the checkout flows join as migration proceeds). Only the
// module facade may reach it.
package userorder

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type Service struct {
	orders repository.OrderRepo
}

func NewService(orders repository.OrderRepo) *Service {
	return &Service{orders: orders}
}

// QueryDetail returns one of the current user's orders; ownership is
// enforced here and the referrer commission never leaves the module.
func (s *Service) QueryDetail(ctx context.Context, req *dto.QueryOrderDetailRequest) (*dto.OrderDetail, error) {
	currentUser, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok || currentUser == nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	orderInfo, err := s.orders.FindOneDetailsByOrderNo(ctx, req.OrderNo)
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryOrderDetail] Database query error", logger.Field("error", err.Error()), logger.Field("order_no", req.OrderNo))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find order error: %v", err.Error())
	}
	if orderInfo.UserId != currentUser.Id {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "order does not belong to the current user")
	}
	resp := &dto.OrderDetail{}
	tool.DeepCopy(resp, orderInfo)
	// Prevent commission amount leakage
	resp.Commission = 0
	return resp, nil
}

func (s *Service) QueryList(ctx context.Context, req *dto.QueryOrderListRequest) (*dto.QueryOrderListResponse, error) {
	u, ok := ctx.Value(constant.CtxKeyUser).(*user.User)
	if !ok {
		logger.Error("current user is not found in context")
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "Invalid Access")
	}
	total, data, err := s.orders.QueryOrderListByPage(ctx, req.Page, req.Size, 0, u.Id, 0, "")
	if err != nil {
		logger.WithContext(ctx).Errorw("[QueryOrderListLogic] Query order list failed", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Query order list failed")
	}
	resp := &dto.QueryOrderListResponse{
		Total: total,
		List:  make([]dto.OrderDetail, 0),
	}
	for _, item := range data {
		var orderInfo dto.OrderDetail
		tool.DeepCopy(&orderInfo, item)
		// Prevent commission amount leakage
		orderInfo.Commission = 0
		resp.List = append(resp.List, orderInfo)
	}
	return resp, nil
}
