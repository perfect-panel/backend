// Package userorder implements the user-facing order query subdomain of the
// billing module. Only the module facade may reach it.
package userorder

import (
	"context"
	"errors"

	"github.com/perfect-panel/backend/internal/infra/mapping"
	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	"github.com/perfect-panel/backend/internal/module/billing/entity/order"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	subscribeEntity "github.com/perfect-panel/backend/internal/module/subscription/entity/subscribe"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/xerr"
	"gorm.io/gorm"
)

// PlanReader is the subdomain's read-only port onto the subscription
// domain's plan catalogue: the order rows only carry the plan ID, and the
// plan fields shown on an order detail are attached here instead of through
// a cross-domain SQL association (ADR-001 step 5).
type PlanReader interface {
	FindOne(ctx context.Context, id int64) (*subscribeEntity.Subscribe, error)
}

// Orders reads the current user's orders.
type Orders interface {
	FindOneDetailsByOrderNo(ctx context.Context, orderNo string) (*order.Details, error)
	QueryOrderListByPage(ctx context.Context, page, size int, status uint8, user, subscribe int64, search string) (int64, []*order.Details, error)
}

type Service struct {
	orders Orders
	plans  PlanReader
}

func NewService(orders Orders, plans PlanReader) *Service {
	return &Service{orders: orders, plans: plans}
}

// attachPlan fills the detail's plan fields from the subscription domain.
// A missing plan (deleted, or a recharge order without one) leaves the
// zero value, matching the former SQL association's behaviour.
func (s *Service) attachPlan(ctx context.Context, detail *dto.OrderDetail, cache map[int64]*subscribeEntity.Subscribe) error {
	if detail.SubscribeId == 0 || s.plans == nil {
		return nil
	}
	plan, cached := cache[detail.SubscribeId]
	if !cached {
		found, err := s.plans.FindOne(ctx, detail.SubscribeId)
		if err != nil {
			logger.WithContext(ctx).Errorw("[UserOrder] load plan for order failed",
				logger.Field("error", err.Error()), logger.Field("subscribe_id", detail.SubscribeId))
		} else {
			plan = found
		}
		cache[detail.SubscribeId] = plan
	}
	if plan == nil {
		return nil
	}
	if err := mapping.Copy(&detail.Subscribe, plan); err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "map subscribe %d of order %s", plan.Id, detail.OrderNo)
	}
	return nil
}

// orderDetail is what the buyer is shown of an order: the order with its
// plan, never the referrer commission it earned.
func (s *Service) orderDetail(ctx context.Context, orderInfo *order.Details, planCache map[int64]*subscribeEntity.Subscribe) (dto.OrderDetail, error) {
	var detail dto.OrderDetail
	if err := mapping.Copy(&detail, orderInfo); err != nil {
		return dto.OrderDetail{}, xerr.Wrapf(err, xerr.ERROR, "map order %s", orderInfo.OrderNo)
	}
	if err := s.attachPlan(ctx, &detail, planCache); err != nil {
		return dto.OrderDetail{}, err
	}
	detail.Commission = 0
	return detail, nil
}

// QueryDetail returns one of the current user's orders; ownership is
// enforced here and the referrer commission never leaves the module.
func (s *Service) QueryDetail(ctx context.Context, req *dto.QueryOrderDetailRequest) (*dto.OrderDetail, error) {
	currentUser, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	orderInfo, err := s.orders.FindOneDetailsByOrderNo(ctx, req.OrderNo)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, xerr.Errorf(xerr.OrderNotExist, "order %s not found", req.OrderNo)
	}
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find order %s", req.OrderNo)
	}
	if orderInfo.UserId != currentUser.Id {
		return nil, xerr.Errorf(xerr.InvalidAccess, "order does not belong to the current user")
	}
	detail, err := s.orderDetail(ctx, orderInfo, map[int64]*subscribeEntity.Subscribe{})
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func (s *Service) QueryList(ctx context.Context, req *dto.QueryOrderListRequest) (*dto.QueryOrderListResponse, error) {
	u, ok := user.FromContext(ctx)
	if !ok {
		return nil, xerr.Errorf(xerr.InvalidAccess, "Invalid Access")
	}
	total, data, err := s.orders.QueryOrderListByPage(ctx, req.Page, req.Size, 0, u.Id, 0, "")
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "query orders of user %d", u.Id)
	}
	resp := &dto.QueryOrderListResponse{
		Total: total,
		List:  make([]dto.OrderDetail, 0),
	}
	planCache := map[int64]*subscribeEntity.Subscribe{}
	for _, item := range data {
		detail, err := s.orderDetail(ctx, item, planCache)
		if err != nil {
			return nil, err
		}
		resp.List = append(resp.List, detail)
	}
	return resp, nil
}
