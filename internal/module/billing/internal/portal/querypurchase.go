package portal

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

// Centralized error handler for database issues
func wrapDatabaseError(err error) error {
	return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Database Query Error: %v", err.Error())
}

// QueryPurchaseOrder returns the guest order's status snapshot and, once the
// account exists, the exchanged session token.
func (s *Service) QueryPurchaseOrder(ctx context.Context, req *dto.QueryPurchaseOrderRequest) (*dto.QueryPurchaseOrderResponse, error) {
	orderInfo, err := s.deps.Orders.FindOneByOrderNo(ctx, req.OrderNo)
	if err != nil {
		return nil, wrapDatabaseError(err)
	}
	if err := s.authorizePurchaseOrder(ctx, orderInfo, req); err != nil {
		return nil, err
	}
	// Handle temporary orders if applicable
	var token string
	if orderInfo.Status == 2 || orderInfo.Status == 5 {
		if orderInfo.UserId == 0 {
			return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "guest account is not ready")
		}
		if token, err = s.IssueSession(ctx, orderInfo.UserId); err != nil {
			return nil, err
		}
	}
	// Fetch subscription and payment information
	subscribeInfo, paymentInfo, err := s.fetchOrderDetails(ctx, orderInfo)
	if err != nil {
		return nil, err
	}

	return &dto.QueryPurchaseOrderResponse{
		OrderNo:        orderInfo.OrderNo,
		Subscribe:      subscribeInfo,
		Quantity:       orderInfo.Quantity,
		Price:          orderInfo.Price,
		Amount:         orderInfo.Amount,
		Discount:       orderInfo.Discount,
		Coupon:         orderInfo.Coupon,
		CouponDiscount: orderInfo.CouponDiscount,
		FeeAmount:      orderInfo.FeeAmount,
		Payment:        paymentInfo,
		Status:         orderInfo.Status,
		CreatedAt:      orderInfo.CreatedAt.UnixMilli(),
		Token:          token,
	}, nil
}

// authorizePurchaseOrder accepts either the authenticated owner of a completed
// guest order or the unguessable checkout capability issued when that order was
// created.  An email/identifier is not authentication and must never be used to
// mint a session token.
func (s *Service) authorizePurchaseOrder(ctx context.Context, orderInfo *order.Order, req *dto.QueryPurchaseOrderRequest) error {
	if orderInfo.UserId != 0 {
		if currentUser, ok := ctx.Value(constant.CtxKeyUser).(*user.User); ok && currentUser.Id == orderInfo.UserId {
			return nil
		}
	}
	if req.CheckoutToken == "" {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is required")
	}
	if orderInfo.GuestCheckoutTokenHash != "" {
		if subtle.ConstantTimeCompare([]byte(orderInfo.GuestCheckoutTokenHash), []byte(constant.CheckoutTokenHash(req.CheckoutToken))) != 1 {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
		}
		return nil
	}
	// Compatibility for orders created before guest details were made durable.
	cacheKey := fmt.Sprintf(constant.TempOrderCacheKey, orderInfo.OrderNo)
	cacheValue, err := s.deps.GuestCheckoutCache.Get(ctx, cacheKey).Result()
	if err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	var tempOrder constant.TemporaryOrderInfo
	if err := json.Unmarshal([]byte(cacheValue), &tempOrder); err != nil {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	if tempOrder.OrderNo != orderInfo.OrderNo || tempOrder.CheckoutToken == "" ||
		subtle.ConstantTimeCompare([]byte(tempOrder.CheckoutToken), []byte(req.CheckoutToken)) != 1 {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "guest checkout token is invalid")
	}
	return nil
}

// fetchOrderDetails retrieves subscription and payment details
func (s *Service) fetchOrderDetails(ctx context.Context, orderInfo *order.Order) (dto.Subscribe, dto.PaymentMethod, error) {
	sub, err := s.deps.Plans.FindOne(ctx, orderInfo.SubscribeId)
	if err != nil {
		return dto.Subscribe{}, dto.PaymentMethod{}, wrapDatabaseError(err)
	}

	var subscribeInfo dto.Subscribe
	tool.DeepCopy(&subscribeInfo, sub)

	payment, err := s.deps.Payments.FindOne(ctx, orderInfo.PaymentId)
	if err != nil {
		return dto.Subscribe{}, dto.PaymentMethod{}, wrapDatabaseError(err)
	}

	var paymentInfo dto.PaymentMethod
	tool.DeepCopy(&paymentInfo, payment)

	return subscribeInfo, paymentInfo, nil
}
