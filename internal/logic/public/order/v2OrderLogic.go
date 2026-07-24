package order

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/model/dto"
	orderEntity "github.com/perfect-panel/server/internal/model/entity/order"
	"github.com/perfect-panel/server/internal/model/entity/user"
	"github.com/perfect-panel/server/internal/module/billing"
	"github.com/perfect-panel/server/internal/orderflow"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/jwt"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const (
	v2OrderTypePurchase     = "purchase"
	v2OrderTypeRenewal      = "renewal"
	v2OrderTypeResetTraffic = "reset_traffic"
	v2OrderTypeRecharge     = "recharge"

	v2EventScope       = "order-events:read"
	v2EventTicketExtra = 10 * time.Minute
)

// ErrIdempotencyKeyReused is handled as HTTP 409 by the V2 handler. It is a
// distinct transport condition: the original order remains intact.
var ErrIdempotencyKeyReused = stdErrors.New("idempotency key reused with a different request")

type V2OrderLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewV2OrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *V2OrderLogic {
	return &V2OrderLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// CreateAndCheckout is the V2 orchestration boundary. Existing domain
// creators still own pricing, inventory and coupon policy; this method only
// gives them an idempotency context and immediately starts checkout.
func (l *V2OrderLogic) CreateAndCheckout(req *dto.V2CreateOrderRequest, idempotencyKey string) (*dto.V2OrderResponse, error) {
	if err := validateV2CreateRequest(req, l.currentUser()); err != nil {
		return nil, err
	}
	hash, err := l.requestHash(req)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid order request")
	}

	orderInfo, err := l.svcCtx.Store.Order().FindOneByIdempotencyKey(l.ctx, idempotencyKey)
	if err == nil {
		if !sameIdempotencyHash(orderInfo.IdempotencyHash, hash) {
			return nil, ErrIdempotencyKeyReused
		}
		checkoutToken := l.guestCheckoutToken(idempotencyKey, orderInfo)
		if err := l.authorizeExistingCreate(orderInfo, req, checkoutToken); err != nil {
			return nil, err
		}
		return l.checkoutResponse(orderInfo, checkoutToken, req.ReturnURL)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find idempotent order: %v", err)
	}

	checkoutToken := l.derivedGuestCheckoutToken(idempotencyKey)
	meta := orderflow.Idempotency{Key: idempotencyKey, Hash: hash}
	if l.currentUser() == nil {
		meta.GuestCheckoutToken = checkoutToken
	}
	createCtx := orderflow.WithIdempotency(l.ctx, meta)
	orderNo, createdCheckoutToken, err := l.createOrder(createCtx, req)
	if err != nil {
		// A concurrent request with this key can win after our initial lookup.
		// Its transaction owns all reservations; this attempt rolls back before
		// returning the duplicate-key error.
		existing, findErr := l.svcCtx.Store.Order().FindOneByIdempotencyKey(l.ctx, idempotencyKey)
		if findErr == nil {
			if !sameIdempotencyHash(existing.IdempotencyHash, hash) {
				return nil, ErrIdempotencyKeyReused
			}
			return l.checkoutResponse(existing, l.guestCheckoutToken(idempotencyKey, existing), req.ReturnURL)
		}
		return nil, err
	}
	if createdCheckoutToken != "" {
		checkoutToken = createdCheckoutToken
	}
	orderInfo, err = l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "load created order: %v", err)
	}
	return l.checkoutResponse(orderInfo, checkoutToken, req.ReturnURL)
}

func (l *V2OrderLogic) Checkout(orderNo string, req *dto.V2CheckoutOrderRequest) (*dto.V2OrderResponse, error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not found")
	}
	if err := l.authorizeOrder(orderInfo, req.CheckoutToken); err != nil {
		return nil, err
	}
	if orderInfo.Status != 1 {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "order is not pending")
	}
	return l.checkoutResponse(orderInfo, req.CheckoutToken, req.ReturnURL)
}

func (l *V2OrderLogic) GetOrder(orderNo, checkoutToken string) (*dto.V2OrderResponse, error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not found")
	}
	if err := l.authorizeOrder(orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	ticket, expiresAt, err := l.mintEventTicket(orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderResponse{
		Order:  l.snapshot(orderInfo),
		Events: l.eventResponse(orderInfo.OrderNo, ticket, expiresAt),
	}, nil
}

func (l *V2OrderLogic) EventTicket(orderNo, checkoutToken string) (*dto.V2EventTicketResponse, error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not found")
	}
	if err := l.authorizeOrder(orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	ticket, expiresAt, err := l.mintEventTicket(orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2EventTicketResponse{
		URL:             l.eventResponse(orderNo, ticket, expiresAt).URL,
		TicketExpiresAt: expiresAt,
	}, nil
}

// Session exchanges the durable guest checkout capability for an ordinary
// session after activation has created the account.  It is intentionally a
// separate JSON endpoint: a long-lived access token must never appear in a
// browser-visible EventSource URL or SSE event payload.
func (l *V2OrderLogic) Session(orderNo, checkoutToken string) (*dto.V2OrderSessionResponse, error) {
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not found")
	}
	if err := l.authorizeOrder(orderInfo, checkoutToken); err != nil {
		return nil, err
	}
	if orderInfo.GuestCheckoutTokenHash == "" {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "order does not have a guest checkout capability")
	}
	if orderInfo.UserId == 0 || (orderInfo.Status != 2 && orderInfo.Status != 5) {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderStatusError), "guest account is not ready")
	}
	token, err := l.svcCtx.Billing.IssuePortalSession(l.ctx, orderInfo.UserId)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderSessionResponse{AccessToken: token}, nil
}

func (l *V2OrderLogic) createOrder(ctx context.Context, req *dto.V2CreateOrderRequest) (orderNo, checkoutToken string, err error) {
	switch req.Type {
	case v2OrderTypePurchase:
		if l.currentUser() == nil {
			resp, e := l.svcCtx.Billing.PortalPurchase(ctx, &dto.PortalPurchaseRequest{
				AuthType: req.Guest.AuthType, Identifier: req.Guest.Identifier, Password: req.Guest.Password,
				Payment: req.PaymentID, SubscribeId: req.SubscribeID, Quantity: req.Quantity,
				Coupon: req.Coupon, InviteCode: req.Guest.InviteCode,
			})
			if e != nil {
				return "", "", e
			}
			return resp.OrderNo, resp.CheckoutToken, nil
		}
		resp, e := l.svcCtx.Billing.Purchase(ctx, &dto.PurchaseOrderRequest{
			SubscribeId: req.SubscribeID, Quantity: req.Quantity, Payment: req.PaymentID, Coupon: req.Coupon,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeRenewal:
		resp, e := l.svcCtx.Billing.Renewal(ctx, &dto.RenewalOrderRequest{
			UserSubscribeID: req.UserSubscribeID, Quantity: req.Quantity, Payment: req.PaymentID, Coupon: req.Coupon,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeResetTraffic:
		resp, e := l.svcCtx.Billing.ResetTraffic(ctx, &dto.ResetTrafficOrderRequest{
			UserSubscribeID: req.UserSubscribeID, Payment: req.PaymentID,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	case v2OrderTypeRecharge:
		resp, e := l.svcCtx.Billing.Recharge(ctx, &dto.RechargeOrderRequest{
			Amount: req.Amount, Payment: req.PaymentID,
		})
		if e != nil {
			return "", "", e
		}
		return resp.OrderNo, "", nil
	default:
		return "", "", errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "unsupported order type")
	}
}

func (l *V2OrderLogic) checkoutResponse(orderInfo *orderEntity.Order, checkoutToken, returnURL string) (*dto.V2OrderResponse, error) {
	var paymentResp *dto.V2OrderPayment
	if orderInfo.Status == 1 {
		checkout, err := l.svcCtx.Billing.PortalCheckout(l.ctx, &dto.CheckoutOrderRequest{
			OrderNo: orderInfo.OrderNo, CheckoutToken: checkoutToken, ReturnUrl: returnURL,
		})
		if err != nil {
			return nil, err
		}
		paymentResp = &dto.V2OrderPayment{
			Type: checkout.Type, CheckoutURL: checkout.CheckoutUrl, Stripe: checkout.Stripe,
			PaymentStatus: v2PaymentStatus(orderInfo.Status),
		}
		latest, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderInfo.OrderNo)
		if err == nil {
			orderInfo = latest
			paymentResp.PaymentStatus = v2PaymentStatus(orderInfo.Status)
		}
	}
	ticket, expiresAt, err := l.mintEventTicket(orderInfo, checkoutToken)
	if err != nil {
		return nil, err
	}
	return &dto.V2OrderResponse{
		Order:         l.snapshot(orderInfo),
		Payment:       paymentResp,
		Events:        l.eventResponse(orderInfo.OrderNo, ticket, expiresAt),
		CheckoutToken: checkoutTokenForResponse(orderInfo, checkoutToken),
	}, nil
}

func (l *V2OrderLogic) snapshot(orderInfo *orderEntity.Order) dto.V2OrderSnapshot {
	return dto.V2OrderSnapshot{
		OrderNo: orderInfo.OrderNo, Status: v2OrderStatus(orderInfo.Status),
		PaymentStatus: v2PaymentStatus(orderInfo.Status), FulfillmentStatus: v2FulfillmentStatus(orderInfo.Status),
		StateVersion: orderInfo.StateVersion, Amount: orderInfo.Amount,
		Currency:  l.svcCtx.Config.Currency.Unit,
		ExpiresAt: orderInfo.CreatedAt.Add(billing.CloseOrderTimeMinutes * time.Minute).Unix(),
	}
}

// Snapshot exposes the event-stream's current-state payload without granting
// access by itself; callers must authorize the ticket before using it.
func (l *V2OrderLogic) Snapshot(orderInfo *orderEntity.Order) dto.V2OrderSnapshot {
	return l.snapshot(orderInfo)
}

func (l *V2OrderLogic) eventResponse(orderNo, ticket string, expiresAt int64) dto.V2OrderEvents {
	return dto.V2OrderEvents{
		URL:             fmt.Sprintf("/v2/public/orders/%s/events?ticket=%s", url.PathEscape(orderNo), url.QueryEscape(ticket)),
		TicketExpiresAt: expiresAt,
	}
}

func (l *V2OrderLogic) authorizeExistingCreate(orderInfo *orderEntity.Order, req *dto.V2CreateOrderRequest, checkoutToken string) error {
	if l.currentUser() != nil {
		return l.authorizeOrder(orderInfo, "")
	}
	if req.Guest == nil || orderInfo.GuestAuthType != req.Guest.AuthType || orderInfo.GuestIdentifier != req.Guest.Identifier {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "order does not belong to this checkout")
	}
	return l.authorizeOrder(orderInfo, checkoutToken)
}

func (l *V2OrderLogic) authorizeOrder(orderInfo *orderEntity.Order, checkoutToken string) error {
	if currentUser := l.currentUser(); orderInfo.UserId != 0 && currentUser != nil && currentUser.Id == orderInfo.UserId {
		return nil
	}
	if guestCheckoutTokenMatches(orderInfo, checkoutToken) {
		return nil
	}
	return errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "order does not belong to the current user")
}

func (l *V2OrderLogic) mintEventTicket(orderInfo *orderEntity.Order, checkoutToken string) (string, int64, error) {
	if err := l.authorizeOrder(orderInfo, checkoutToken); err != nil {
		return "", 0, err
	}
	expiresAt := orderInfo.CreatedAt.Add((billing.CloseOrderTimeMinutes * time.Minute) + v2EventTicketExtra)
	if expiresAt.Before(time.Now()) {
		expiresAt = time.Now().Add(v2EventTicketExtra)
	}
	seconds := int64(time.Until(expiresAt).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	ticket, err := jwt.NewJwtToken(l.svcCtx.Config.JwtAuth.AccessSecret, time.Now().Unix(), seconds,
		jwt.WithOption("OrderNo", orderInfo.OrderNo),
		jwt.WithOption("Scope", v2EventScope),
		jwt.WithOption("UserId", orderInfo.UserId),
		jwt.WithOption("GuestCheckoutHash", orderInfo.GuestCheckoutTokenHash),
	)
	if err != nil {
		return "", 0, errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "create event ticket")
	}
	return ticket, expiresAt.Unix(), nil
}

// AuthorizeEventTicket validates the self-contained stream capability against
// the current order row. It deliberately does not require a long-lived bearer
// token in the EventSource URL.
func (l *V2OrderLogic) AuthorizeEventTicket(orderNo, ticket string) (*orderEntity.Order, error) {
	claims, err := jwt.ParseJwtToken(ticket, l.svcCtx.Config.JwtAuth.AccessSecret)
	if err != nil || claimString(claims, "OrderNo") != orderNo || claimString(claims, "Scope") != v2EventScope {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "event ticket is invalid")
	}
	orderInfo, err := l.svcCtx.Store.Order().FindOneByOrderNo(l.ctx, orderNo)
	if err != nil {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.OrderNotExist), "order not found")
	}
	if guestCheckoutHashMatches(orderInfo, claimString(claims, "GuestCheckoutHash")) {
		return orderInfo, nil
	}
	if orderInfo.UserId == 0 || claimInt64(claims, "UserId") != orderInfo.UserId {
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "event ticket is invalid")
	}
	return orderInfo, nil
}

func (l *V2OrderLogic) EventTicketExpiresAt(ticket string) (time.Time, error) {
	claims, err := jwt.ParseJwtToken(ticket, l.svcCtx.Config.JwtAuth.AccessSecret)
	if err != nil {
		return time.Time{}, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "event ticket is invalid")
	}
	expiresAt := claimInt64(claims, "exp")
	if expiresAt <= time.Now().Unix() {
		return time.Time{}, errors.Wrapf(xerr.NewErrCode(xerr.InvalidAccess), "event ticket expired")
	}
	return time.Unix(expiresAt, 0), nil
}

func (l *V2OrderLogic) currentUser() *user.User {
	currentUser, _ := l.ctx.Value(constant.CtxKeyUser).(*user.User)
	return currentUser
}

func (l *V2OrderLogic) requestHash(req *dto.V2CreateOrderRequest) (string, error) {
	canonical := struct {
		Type            string
		PaymentID       int64
		SubscribeID     int64
		UserSubscribeID int64
		Quantity        int64
		Coupon          string
		Amount          int64
		UserID          int64
		Guest           *dto.V2GuestOrderRequest
	}{
		Type: req.Type, PaymentID: req.PaymentID, SubscribeID: req.SubscribeID,
		UserSubscribeID: req.UserSubscribeID, Quantity: req.Quantity, Coupon: req.Coupon,
		Amount: req.Amount, Guest: req.Guest,
	}
	if currentUser := l.currentUser(); currentUser != nil {
		canonical.UserID = currentUser.Id
		canonical.Guest = nil
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (l *V2OrderLogic) derivedGuestCheckoutToken(idempotencyKey string) string {
	mac := hmac.New(sha256.New, []byte(l.svcCtx.Config.JwtAuth.AccessSecret))
	_, _ = mac.Write([]byte("v2-guest-checkout:" + idempotencyKey))
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *V2OrderLogic) guestCheckoutToken(idempotencyKey string, orderInfo *orderEntity.Order) string {
	if orderInfo.GuestCheckoutTokenHash == "" {
		return ""
	}
	token := l.derivedGuestCheckoutToken(idempotencyKey)
	if !guestCheckoutTokenMatches(orderInfo, token) {
		return ""
	}
	return token
}

func validateV2CreateRequest(req *dto.V2CreateOrderRequest, currentUser *user.User) error {
	if req == nil || req.PaymentID <= 0 {
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "payment_id is required")
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	switch req.Type {
	case v2OrderTypePurchase:
		if req.SubscribeID <= 0 || req.Quantity <= 0 || req.Quantity > billing.MaxQuantity || req.UserSubscribeID != 0 || req.Amount != 0 {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid purchase parameters")
		}
		if currentUser == nil {
			if req.Guest == nil || strings.TrimSpace(req.Guest.AuthType) == "" || strings.TrimSpace(req.Guest.Identifier) == "" || len(req.Guest.Password) < 8 || len(req.Guest.Password) > 128 {
				return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "guest credentials are required")
			}
		} else if req.Guest != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "guest is only allowed for anonymous purchase")
		}
	case v2OrderTypeRenewal:
		if currentUser == nil || req.UserSubscribeID <= 0 || req.Quantity <= 0 || req.Quantity > billing.MaxQuantity || req.SubscribeID != 0 || req.Amount != 0 || req.Guest != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid renewal parameters")
		}
	case v2OrderTypeResetTraffic:
		if currentUser == nil || req.UserSubscribeID <= 0 || req.SubscribeID != 0 || req.Quantity != 0 || req.Amount != 0 || req.Coupon != "" || req.Guest != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid reset traffic parameters")
		}
	case v2OrderTypeRecharge:
		if currentUser == nil || req.Amount <= 0 || req.SubscribeID != 0 || req.UserSubscribeID != 0 || req.Quantity != 0 || req.Coupon != "" || req.Guest != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "invalid recharge parameters")
		}
	default:
		return errors.Wrapf(xerr.NewErrCode(xerr.InvalidParams), "unsupported order type")
	}
	return nil
}

func sameIdempotencyHash(left, right string) bool {
	return len(left) == len(right) && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func checkoutTokenForResponse(orderInfo *orderEntity.Order, checkoutToken string) string {
	if guestCheckoutTokenMatches(orderInfo, checkoutToken) {
		return checkoutToken
	}
	return ""
}

func guestCheckoutTokenMatches(orderInfo *orderEntity.Order, checkoutToken string) bool {
	if checkoutToken == "" || orderInfo.GuestCheckoutTokenHash == "" {
		return false
	}
	return guestCheckoutHashMatches(orderInfo, constant.CheckoutTokenHash(checkoutToken))
}

func guestCheckoutHashMatches(orderInfo *orderEntity.Order, checkoutHash string) bool {
	return checkoutHash != "" && orderInfo.GuestCheckoutTokenHash != "" &&
		subtle.ConstantTimeCompare([]byte(orderInfo.GuestCheckoutTokenHash), []byte(checkoutHash)) == 1
}

func v2OrderStatus(status uint8) string {
	switch status {
	case 1:
		return "pending_payment"
	case 2:
		return "paid"
	case 3:
		return "closed"
	case 4:
		return "failed"
	case 5:
		return "finished"
	default:
		return "unknown"
	}
}

func v2PaymentStatus(status uint8) string {
	switch status {
	case 2, 5:
		return "paid"
	case 3:
		return "closed"
	case 4:
		return "failed"
	default:
		return "pending"
	}
}

func v2FulfillmentStatus(status uint8) string {
	switch status {
	case 2:
		return "pending"
	case 5:
		return "finished"
	default:
		return "not_started"
	}
}

func claimString(claims map[string]interface{}, name string) string {
	value, _ := claims[name].(string)
	return value
}

func claimInt64(claims map[string]interface{}, name string) int64 {
	switch value := claims[name].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case json.Number:
		result, _ := value.Int64()
		return result
	default:
		return 0
	}
}
