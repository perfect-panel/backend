package portal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/perfect-panel/server/internal/model/order"

	"github.com/perfect-panel/server/pkg/timeutil"
	"github.com/perfect-panel/server/pkg/tool"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/internal/types"
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

func (l *QueryPurchaseOrderLogic) QueryPurchaseOrder(req *types.QueryPurchaseOrderRequest) (resp *types.QueryPurchaseOrderResponse, err error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, req.OrderNo)
	if err != nil {
		return nil, wrapDatabaseError(err)
	}
	// Handle temporary orders if applicable
	var token string
	if orderInfo.Status == 2 || orderInfo.Status == 5 {
		if token, err = l.handleTemporaryOrder(orderInfo, req); err != nil {
			return nil, err
		}
	}
	// Fetch subscription and payment information
	subscribeInfo, paymentInfo, err := l.fetchOrderDetails(orderInfo)
	if err != nil {
		return nil, err
	}

	return &types.QueryPurchaseOrderResponse{
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

// handleTemporaryOrder processes temporary order-related operations
func (l *QueryPurchaseOrderLogic) handleTemporaryOrder(orderInfo *order.Order, req *types.QueryPurchaseOrderRequest) (string, error) {
	cacheKey := fmt.Sprintf(constant.TempOrderCacheKey, orderInfo.OrderNo)
	cacheValue, err := l.svcCtx.Redis.Get(l.ctx, cacheKey).Result()
	if err != nil {
		l.Errorw("Get TempOrderCacheKey Error", logger.Field("cacheKey", cacheKey), logger.Field("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Get TempOrderCacheKey Error: %v", err.Error())
	}

	var tempOrder constant.TemporaryOrderInfo
	if err := json.Unmarshal([]byte(cacheValue), &tempOrder); err != nil {
		l.Errorw("JSON Unmarshal Error", logger.Field("error", err.Error()), logger.Field("cacheValue", cacheValue))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "JSON Unmarshal Error: %v", err.Error())
	}
	if tempOrder.OrderNo != orderInfo.OrderNo {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Order number mismatch")
	}

	// Validate user and email
	if err = l.validateUserAndEmail(orderInfo, req.AuthType, req.Identifier); err != nil {
		return "", err
	}

	// Generate session token
	return l.generateSessionToken(orderInfo.UserId)
}

// validateUserAndEmail ensures the user and email are correct
func (l *QueryPurchaseOrderLogic) validateUserAndEmail(orderInfo *order.Order, platform, openid string) error {
	userInfo, err := l.svcCtx.Store.User().FindOne(l.ctx, orderInfo.UserId)
	if err != nil {
		return wrapDatabaseError(err)
	}

	authMethod, err := l.svcCtx.Store.User().FindUserAuthMethodByOpenID(l.ctx, platform, openid)
	if err != nil {
		return wrapDatabaseError(err)
	}
	if authMethod.UserId != userInfo.Id {
		return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Email verification failed")
	}

	return nil
}

// generateSessionToken creates a session token and stores it in Redis
func (l *QueryPurchaseOrderLogic) generateSessionToken(userId int64) (string, error) {
	sessionId := uuidx.NewUUID().String()
	token, err := jwt.NewJwtToken(
		l.svcCtx.Config.JwtAuth.AccessSecret,
		timeutil.Now().Unix(),
		l.svcCtx.Config.JwtAuth.AccessExpire,
		jwt.WithOption("UserId", userId),
		jwt.WithOption("SessionId", sessionId),
	)
	if err != nil {
		l.Errorw("Token Generation Error", logger.Field("error", err.Error()))
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Token generation error")
	}

	cacheKey := fmt.Sprintf("%v:%v", config.SessionIdKey, sessionId)
	if err := l.svcCtx.Redis.Set(l.ctx, cacheKey, userId, time.Duration(l.svcCtx.Config.JwtAuth.AccessExpire)*time.Second).Err(); err != nil {
		return "", errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "Session storage error")
	}

	return token, nil
}

// fetchOrderDetails retrieves subscription and payment details
func (l *QueryPurchaseOrderLogic) fetchOrderDetails(orderInfo *order.Order) (types.Subscribe, types.PaymentMethod, error) {
	sub, err := l.svcCtx.Store.Subscribe().FindOne(l.ctx, orderInfo.SubscribeId)
	if err != nil {
		return types.Subscribe{}, types.PaymentMethod{}, wrapDatabaseError(err)
	}

	var subscribeInfo types.Subscribe
	tool.DeepCopy(&subscribeInfo, sub)

	payment, err := l.svcCtx.Store.Payment().FindOne(l.ctx, orderInfo.PaymentId)
	if err != nil {
		return types.Subscribe{}, types.PaymentMethod{}, wrapDatabaseError(err)
	}

	var paymentInfo types.PaymentMethod
	tool.DeepCopy(&paymentInfo, payment)

	return subscribeInfo, paymentInfo, nil
}
