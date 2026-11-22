// Package adminpayment implements the payment-method management subdomain of
// the billing module. Only the module facade may reach it.
package adminpayment

import (
	"context"
	"encoding/json"

	dto "github.com/perfect-panel/backend/internal/module/billing/contract"
	paymentModel "github.com/perfect-panel/backend/internal/module/billing/entity/payment"
	"github.com/perfect-panel/backend/internal/module/billing/internal/gateway"
	"github.com/perfect-panel/backend/internal/module/billing/internal/payment"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// Payments is the payment-method persistence administration needs.
type Payments interface {
	Insert(ctx context.Context, data *paymentModel.Payment) error
	FindOne(ctx context.Context, id int64) (*paymentModel.Payment, error)
	Update(ctx context.Context, data *paymentModel.Payment) error
	Delete(ctx context.Context, id int64) error
	FindListByPage(ctx context.Context, page, size int, req *paymentModel.Filter) (int64, []*paymentModel.Payment, error)
}

// PendingOrders counts the unpaid orders bound to a payment method.
type PendingOrders interface {
	CountPendingByPaymentID(ctx context.Context, paymentID int64) (int64, error)
}

type Deps struct {
	Payments Payments
	Orders   PendingOrders
	// Gateways validates configurations and manages Stripe webhooks; nil
	// selects the production gateways.
	Gateways *gateway.Registry
	// SiteHost reads the public site host a callback URL falls back to when
	// the method has no domain.
	SiteHost func() string
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) *Service {
	if deps.Gateways == nil {
		deps.Gateways = gateway.NewRegistry()
	}
	return &Service{deps: deps}
}

func (s *Service) siteHost() string {
	if s.deps.SiteHost == nil {
		return ""
	}
	return s.deps.SiteHost()
}

// A disabled PayPal draft gives the administrator its unique notification
// URL before PayPal issues the webhook ID needed to enable checkout.
func (s *Service) normalizeMethod(platform string, raw any, enable *bool) (string, error) {
	config, err := s.deps.Gateways.NormalizeConfig(platform, raw)
	if err == nil && platform == payment.PayPal.String() && enable != nil && *enable {
		_, err = s.deps.Gateways.Open(&paymentModel.Payment{Platform: platform, Config: config})
	}
	return config, err
}

func (s *Service) Create(ctx context.Context, req *dto.CreatePaymentMethodRequest) (*dto.PaymentConfig, error) {
	if payment.ParsePlatform(req.Platform) == payment.UNSUPPORTED {
		return nil, xerr.Errorf(xerr.UnsupportedPaymentPlatform, "unsupported payment platform: %s", req.Platform)
	}
	if err := validatePaymentFee(req.FeeMode, req.FeePercent, req.FeeAmount); err != nil {
		return nil, err
	}
	config, err := s.normalizeMethod(req.Platform, req.Config, req.Enable)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.InvalidPaymentConfig, "invalid payment config")
	}
	method := &paymentModel.Payment{
		Name:        req.Name,
		Platform:    req.Platform,
		Icon:        req.Icon,
		Domain:      req.Domain,
		Description: req.Description,
		Config:      config,
		FeeMode:     req.FeeMode,
		FeePercent:  req.FeePercent,
		FeeAmount:   req.FeeAmount,
		Sort:        req.Sort,
		Enable:      req.Enable,
		Token:       random.KeyNew(8, 1),
	}
	if payment.ParsePlatform(req.Platform) == payment.Stripe {
		if err := s.createStripeMethod(ctx, method); err != nil {
			return nil, err
		}
	} else if err := s.deps.Payments.Insert(ctx, method); err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseInsertError, "insert payment method")
	}
	return paymentConfigResponse(method), nil
}

// createStripeMethod registers the method's webhook endpoint with Stripe and
// stores the method with the endpoint's signing secret. The endpoint is
// created before the method is saved, outside any transaction, and deleted
// again when the method cannot be saved, so no orphaned endpoint keeps
// receiving events.
func (s *Service) createStripeMethod(ctx context.Context, method *paymentModel.Payment) error {
	var config paymentModel.StripeConfig
	if err := config.Unmarshal([]byte(method.Config)); err != nil {
		return xerr.Wrapf(err, xerr.InvalidPaymentConfig, "invalid Stripe config")
	}
	if config.SecretKey == "" {
		return xerr.Errorf(xerr.InvalidPaymentConfig, "stripe secret key is empty")
	}
	notifyURL, err := gateway.NotifyURL(method, s.siteHost())
	if err != nil {
		return err
	}
	webhooks := s.deps.Gateways.StripeWebhooks(config.SecretKey)
	endpointID, secret, err := webhooks.CreateWebhookEndpoint(ctx, notifyURL)
	if err != nil {
		return xerr.Wrapf(err, xerr.ERROR, "create stripe webhook endpoint")
	}
	config.WebhookSecret, config.WebhookEndpointID = secret, endpointID
	content, err := config.Marshal()
	if err == nil {
		method.Config = string(content)
		err = s.deps.Payments.Insert(ctx, method)
	}
	if err != nil {
		if deleteErr := webhooks.DeleteWebhookEndpoint(ctx, endpointID); deleteErr != nil {
			logger.WithContext(ctx).Errorw("[CreatePaymentMethod] remove the Stripe webhook endpoint of an unsaved payment method by hand",
				logger.Field("endpoint", endpointID), logger.Field("error", deleteErr.Error()))
		}
		return xerr.Wrapf(err, xerr.DatabaseInsertError, "insert payment method")
	}
	return nil
}

// stripeUpdate is the webhook side of a Stripe method's update. A secret key
// that changed selects another Stripe account: the webhook endpoint the
// method's callbacks arrive through lives in the old account, so a new one
// is registered in the new account, its secret stored with the method, and
// the old one removed once the method is saved. An unchanged key keeps the
// endpoint, whose secret and id an administrator's form need not carry.
type stripeUpdate struct {
	old, updated paymentModel.StripeConfig
	webhooks     gateway.StripeWebhooks
	endpointID   string // the endpoint registered by this update, if any
}

// prepareStripeUpdate reads both configurations and, for a changed key,
// registers the webhook endpoint in the new account. It returns the
// configuration to store.
func (s *Service) prepareStripeUpdate(ctx context.Context, method *paymentModel.Payment, config string) (string, *stripeUpdate, error) {
	update := &stripeUpdate{}
	if err := update.old.Unmarshal([]byte(method.Config)); err != nil {
		return "", nil, xerr.Wrapf(err, xerr.InvalidPaymentConfig, "invalid stored Stripe config")
	}
	if err := update.updated.Unmarshal([]byte(config)); err != nil {
		return "", nil, xerr.Wrapf(err, xerr.InvalidPaymentConfig, "invalid Stripe config")
	}
	if update.updated.SecretKey == "" {
		return "", nil, xerr.Errorf(xerr.InvalidPaymentConfig, "stripe secret key is empty")
	}
	if update.updated.SecretKey == update.old.SecretKey {
		if update.updated.WebhookSecret == "" {
			update.updated.WebhookSecret = update.old.WebhookSecret
		}
		if update.updated.WebhookEndpointID == "" {
			update.updated.WebhookEndpointID = update.old.WebhookEndpointID
		}
	} else {
		notifyURL, err := gateway.NotifyURL(method, s.siteHost())
		if err != nil {
			return "", nil, err
		}
		update.webhooks = s.deps.Gateways.StripeWebhooks(update.updated.SecretKey)
		endpointID, secret, err := update.webhooks.CreateWebhookEndpoint(ctx, notifyURL)
		if err != nil {
			return "", nil, xerr.Wrapf(err, xerr.ERROR, "create stripe webhook endpoint")
		}
		update.endpointID = endpointID
		update.updated.WebhookSecret, update.updated.WebhookEndpointID = secret, endpointID
	}
	content, err := update.updated.Marshal()
	if err != nil {
		update.rollback(ctx)
		return "", nil, xerr.Wrapf(err, xerr.InvalidPaymentConfig, "encode Stripe config")
	}
	return string(content), update, nil
}

// rollback removes the endpoint this update registered when the method could
// not be saved, so no orphaned endpoint keeps receiving events.
func (u *stripeUpdate) rollback(ctx context.Context) {
	if u == nil || u.endpointID == "" {
		return
	}
	if err := u.webhooks.DeleteWebhookEndpoint(ctx, u.endpointID); err != nil {
		logger.WithContext(ctx).Errorw("[UpdatePaymentMethod] remove the Stripe webhook endpoint of an unsaved payment method by hand",
			logger.Field("endpoint", u.endpointID), logger.Field("error", err.Error()))
	}
}

// retireOldEndpoint removes the endpoint of the old account once the method
// uses the new one. It is best effort: the old account may already have
// revoked the key, and an endpoint registered before its id was recorded
// cannot be found; both are left for the administrator.
func (s *Service) retireOldEndpoint(ctx context.Context, u *stripeUpdate) {
	if u == nil || u.endpointID == "" {
		return
	}
	if u.old.WebhookEndpointID == "" {
		logger.WithContext(ctx).Infow("[UpdatePaymentMethod] the webhook endpoint of the previous Stripe account is not recorded; remove it in the Stripe dashboard by hand")
		return
	}
	if u.old.SecretKey == "" {
		return
	}
	if err := s.deps.Gateways.StripeWebhooks(u.old.SecretKey).DeleteWebhookEndpoint(ctx, u.old.WebhookEndpointID); err != nil {
		logger.WithContext(ctx).Errorw("[UpdatePaymentMethod] remove the webhook endpoint of the previous Stripe account by hand",
			logger.Field("endpoint", u.old.WebhookEndpointID), logger.Field("error", err.Error()))
	}
}

func (s *Service) Update(ctx context.Context, req *dto.UpdatePaymentMethodRequest) (*dto.PaymentConfig, error) {
	if payment.ParsePlatform(req.Platform) == payment.UNSUPPORTED {
		return nil, xerr.Errorf(xerr.UnsupportedPaymentPlatform, "unsupported payment platform: %s", req.Platform)
	}
	method, err := s.deps.Payments.FindOne(ctx, req.Id)
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method %d", req.Id)
	}
	if method.Platform != req.Platform {
		return nil, xerr.Errorf(xerr.PaymentPlatformImmutable, "payment platform cannot be changed")
	}
	if err := validatePaymentFee(req.FeeMode, req.FeePercent, req.FeeAmount); err != nil {
		return nil, err
	}
	if req.Sort == 0 {
		req.Sort = method.Sort
	}
	// Balance is an internal checkout method: it has no gateway credentials
	// to validate and no callback URL, so toggling it must neither parse a
	// platform config nor be blocked by the pending-order guard.
	config := method.Config
	var stripe *stripeUpdate
	if payment.ParsePlatform(req.Platform) != payment.Balance {
		if config, err = s.normalizeMethod(req.Platform, req.Config, req.Enable); err != nil {
			return nil, xerr.Wrapf(err, xerr.InvalidPaymentConfig, "invalid payment config")
		}
		if method.Config != config || method.Domain != req.Domain {
			if err := s.ensureNoPendingOrders(ctx, method.Id); err != nil {
				return nil, err
			}
		}
		if payment.ParsePlatform(req.Platform) == payment.Stripe {
			// The webhook endpoint follows the account the secret key
			// selects; the call happens outside any transaction.
			if config, stripe, err = s.prepareStripeUpdate(ctx, method, config); err != nil {
				return nil, err
			}
		}
	}
	// The id and the platform are those of the stored method: the platform
	// was checked above and the token never changes.
	method.Name = req.Name
	method.Icon = req.Icon
	method.Domain = req.Domain
	method.Description = req.Description
	method.Config = config
	method.FeeMode = req.FeeMode
	method.FeePercent = req.FeePercent
	method.FeeAmount = req.FeeAmount
	method.Sort = req.Sort
	method.Enable = req.Enable
	if err := s.deps.Payments.Update(ctx, method); err != nil {
		stripe.rollback(ctx)
		return nil, xerr.Wrapf(err, xerr.DatabaseUpdateError, "update payment method %d", req.Id)
	}
	s.retireOldEndpoint(ctx, stripe)
	return paymentConfigResponse(method), nil
}

func (s *Service) Delete(ctx context.Context, req *dto.DeletePaymentMethodRequest) error {
	method, err := s.deps.Payments.FindOne(ctx, req.Id)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method %d", req.Id)
	}
	// The seeded balance method is an internal checkout method the storefront
	// depends on; deleting it breaks every balance purchase with an opaque
	// record-not-found until the seed row is restored by hand.
	if payment.ParsePlatform(method.Platform) == payment.Balance {
		return xerr.Errorf(xerr.PaymentMethodInternal, "the balance payment method cannot be deleted")
	}
	if err := s.ensureNoPendingOrders(ctx, req.Id); err != nil {
		return err
	}
	if err := s.deps.Payments.Delete(ctx, req.Id); err != nil {
		return xerr.Wrapf(err, xerr.DatabaseDeletedError, "delete payment method %d", req.Id)
	}
	return nil
}

// ensureNoPendingOrders refuses to change a method pending orders still
// depend on: their checkout and callbacks use its configuration.
func (s *Service) ensureNoPendingOrders(ctx context.Context, paymentID int64) error {
	pending, err := s.deps.Orders.CountPendingByPaymentID(ctx, paymentID)
	if err != nil {
		return xerr.Wrapf(err, xerr.DatabaseQueryError, "count pending orders of payment method %d", paymentID)
	}
	if pending > 0 {
		return xerr.Errorf(xerr.PaymentMethodHasPendingOrders, "payment method has %d pending orders", pending)
	}
	return nil
}

func (s *Service) List(ctx context.Context, req *dto.GetPaymentMethodListRequest) (*dto.GetPaymentMethodListResponse, error) {
	total, list, err := s.deps.Payments.FindListByPage(ctx, req.Page, req.Size, &paymentModel.Filter{
		Search: req.Search,
		Mark:   req.Platform,
		Enable: req.Enable,
	})
	if err != nil {
		return nil, xerr.Wrapf(err, xerr.DatabaseQueryError, "find payment method list")
	}
	resp := &dto.GetPaymentMethodListResponse{
		Total: total,
		List:  make([]dto.PaymentMethodDetail, len(list)),
	}
	siteHost := s.siteHost()
	for i, v := range list {
		config := make(map[string]any)
		_ = json.Unmarshal([]byte(v.Config), &config)
		resp.List[i] = dto.PaymentMethodDetail{
			Id:          v.Id,
			Name:        v.Name,
			Platform:    v.Platform,
			Icon:        v.Icon,
			Domain:      v.Domain,
			Config:      config,
			FeeMode:     v.FeeMode,
			FeePercent:  v.FeePercent,
			FeeAmount:   v.FeeAmount,
			Sort:        v.Sort,
			Enable:      v.Enable != nil && *v.Enable,
			NotifyURL:   s.displayNotifyURL(v, siteHost),
			Description: v.Description,
		}
	}
	return resp, nil
}

// displayNotifyURL is the callback URL checkout gives the gateway, or empty
// when the method has none: the balance method, or a method whose callback
// cannot be built until a domain or site host is configured.
func (s *Service) displayNotifyURL(method *paymentModel.Payment, siteHost string) string {
	if !s.deps.Gateways.Handles(method.Platform) {
		return ""
	}
	notifyURL, err := gateway.NotifyURL(method, siteHost)
	if err != nil {
		return ""
	}
	return notifyURL
}

func (s *Service) Platforms(_ context.Context) (*dto.PaymentPlatformResponse, error) {
	return &dto.PaymentPlatformResponse{List: payment.GetSupportedPlatforms()}, nil
}

func validatePaymentFee(mode uint, percent, amount int64) error {
	if mode > 3 || percent < 0 || percent > 100 || amount < 0 {
		return xerr.Errorf(xerr.InvalidPaymentFee, "invalid payment fee configuration")
	}
	return nil
}

// paymentConfigResponse describes a saved method to the administrator, its
// stored configuration decoded into JSON fields; the notify token stays out.
func paymentConfigResponse(method *paymentModel.Payment) *dto.PaymentConfig {
	var configMap map[string]any
	_ = json.Unmarshal([]byte(method.Config), &configMap)
	return &dto.PaymentConfig{
		Id:          method.Id,
		Name:        method.Name,
		Platform:    method.Platform,
		Description: method.Description,
		Icon:        method.Icon,
		Domain:      method.Domain,
		Config:      configMap,
		FeeMode:     method.FeeMode,
		FeePercent:  method.FeePercent,
		FeeAmount:   method.FeeAmount,
		Sort:        method.Sort,
		Enable:      method.Enable,
	}
}
