package portal

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"

	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/uuidx"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QueryPurchaseOrderLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewQueryPurchaseOrderLogic Query Purchase Order
func NewQueryPurchaseOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QueryPurchaseOrderLogic {
	return &QueryPurchaseOrderLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Centralized error handler for database issues
func wrapDatabaseError(err error) error {
	return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "Database Query Error: %v", err.Error())
}

func (l *QueryPurchaseOrderLogic) QueryPurchaseOrder(req *dto.QueryPurchaseOrderRequest) (resp *dto.QueryPurchaseOrderResponse, err error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		return nil, wrapDatabaseError(err)
	}
	if err := l.authorizePurchaseOrder(orderInfo, req); err != nil {
		return nil, err
	}
	// Handle temporary orders if applicable
	var token string
	if orderInfo.Status == 2 || orderInfo.Status == 5 {
		if token, err = l.handleTemporaryOrder(orderInfo); err != nil {
			return nil, err
		}
	}
	// Fetch subscription and payment information
	subscribeInfo, paymentInfo, err := l.fetchOrderDetails(orderInfo)
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
func (l *QueryPurchaseOrderLogic) authorizePurchaseOrder(orderInfo *order.Order, req *dto.QueryPurchaseOrderRequest) error {
	if orderInfo.UserId != 0 {
		if currentUser, ok := l.ctx.Value(constant.CtxKeyUser).(*user.User); ok && currentUser.Id == orderInfo.UserId {
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
	cacheValue, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
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

// handleTemporaryOrder processes temporary order-related operations
func (l *QueryPurchaseOrderLogic) handleTemporaryOrder(orderInfo *order.Order) (string, error) {
	if orderInfo.UserId == 0 {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "guest account is not ready")
	}

	// Generate session token
	return l.generateSessionToken(orderInfo.UserId)
}

// generateSessionToken creates a session token and stores it in Redis
func (l *QueryPurchaseOrderLogic) generateSessionToken(userId int64) (string, error) {
	return IssuePurchaseSession(l.ctx, l.svcCtx, userId)
}

// IssuePurchaseSession creates the normal authenticated session issued after a
// guest purchase completes.  Both V1's status endpoint and V2's explicit
// capability-exchange endpoint use this helper so their token and Redis
// session semantics cannot drift.
func IssuePurchaseSession(ctx context.Context, svcCtx *svc.ServiceContext, userId int64) (string, error) {
	sessionId := uuidx.NewUUID().String()
	token, err := jwt.NewJwtToken(
		svcCtx.Config.JwtAuth.AccessSecret,
		timeutil.Now().Unix(),
		svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userId),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Token generation error")
	}

	cacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err := svcCtx.Redis.Set(ctx, cacheKey, userId, time.Duration(svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Session storage error")
	}

	return token, nil
}

// fetchOrderDetails retrieves subscription and payment details
func (l *QueryPurchaseOrderLogic) fetchOrderDetails(orderInfo *order.Order) (dto.Subscribe, dto.PaymentMethod, error) {
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, orderInfo.SubscribeId)
	if err != nil {
		return dto.Subscribe{}, dto.PaymentMethod{}, wrapDatabaseError(err)
	}

	var subscribeInfo dto.Subscribe
	tool.DeepCopy(&subscribeInfo, sub)

	payment, err := l.svcCtx.Store.Payment().FindOne(l.ctx, orderInfo.PaymentId)
	if err != nil {
		return dto.Subscribe{}, dto.PaymentMethod{}, wrapDatabaseError(err)
	}

	var paymentInfo dto.PaymentMethod
	tool.DeepCopy(&paymentInfo, payment)

	return subscribeInfo, paymentInfo, nil
}
