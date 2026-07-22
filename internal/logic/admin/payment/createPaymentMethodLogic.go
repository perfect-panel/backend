package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	paymentModel "github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/repository"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/payment"
	"github.com/perfect-panel/server/pkg/payment/stripe"
	"github.com/perfect-panel/server/pkg/random"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreatePaymentMethodLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreatePaymentMethodLogic Create Payment Method
func NewCreatePaymentMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreatePaymentMethodLogic {
	return &CreatePaymentMethodLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreatePaymentMethodLogic) CreatePaymentMethod(req *dto.CreatePaymentMethodRequest) (resp *dto.PaymentConfig, err error) {
	if payment.ParsePlatform(req.Platform) == payment.UNSUPPORTED {
		l.Errorw("unsupported payment platform", logger.Field("mark", req.Platform))
		return nil, errors.Wrapf(xerr.NewErrCodeMsg(400, "UNSUPPORTED_PAYMENT_PLATFORM"), "unsupported payment platform: %s", req.Platform)
	}
	config := parsePaymentPlatformConfig(l.ctx, payment.ParsePlatform(req.Platform), req.Config)
	var paymentMethod = &paymentModel.Payment{
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
	err = l.svcCtx.Store.InTx(l.ctx, func(store repository.Store) error {
		if req.Platform == "Stripe" {
			var cfg paymentModel.StripeConfig
			if err = cfg.Unmarshal([]byte(paymentMethod.Config)); err != nil {
				l.Errorf("[CreatePaymentMethod] unmarshal stripe config error: %s", err.Error())
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "unmarshal stripe config error: %s", err.Error())
			}
			if cfg.SecretKey == "" {
				l.Error("[CreatePaymentMethod] stripe secret key is empty")
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "stripe secret key is empty")
			}

			// Create Stripe webhook endpoint
			client := stripe.NewClient(stripe.Config{
				SecretKey: cfg.SecretKey,
				PublicKey: cfg.PublicKey,
			})
			url := fmt.Sprintf("%s/v1/notify/Stripe/%s", strings.TrimSuffix(req.Domain, "/"), paymentMethod.Token)
			endpoint, err := client.CreateWebhookEndpoint(url)
			if err != nil {
				l.Errorw("[CreatePaymentMethod] create stripe webhook endpoint error", logger.Field("error", err.Error()))
				return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "create stripe webhook endpoint error: %s", err.Error())
			}
			cfg.WebhookSecret = endpoint.Secret
			content, _ := cfg.Marshal()
			paymentMethod.Config = string(content)
		}
		if err = store.Payment().Insert(l.ctx, paymentMethod); err != nil {
			return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert payment method error: %s", err.Error())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	resp = &dto.PaymentConfig{}
	tool.DeepCopy(resp, paymentMethod)
	var configMap map[string]interface{}
	_ = json.Unmarshal([]byte(paymentMethod.Config), &configMap)
	resp.Config = configMap
	return
}

func parsePaymentPlatformConfig(ctx context.Context, platform payment.Platform, config interface{}) string {
	data, err := json.Marshal(config)
	if err != nil {
		logger.WithContext(ctx).Errorw("marshal config error", logger.Field("platform", platform), logger.Field("error", err.Error()))
		return ""
	}

	// 通用处理函数
	handleConfig := func(name string, target interface {
		Unmarshal([]byte) error
		Marshal() ([]byte, error)
	}) string {
		if err = target.Unmarshal(data); err != nil {
			logger.WithContext(ctx).Errorw("parse "+name+" config error", logger.Field("error", err.Error()))
			return ""
		}
		content, err := target.Marshal()
		if err != nil {
			logger.WithContext(ctx).Errorw("marshal "+name+" config error", logger.Field("error", err.Error()))
			return ""
		}
		return string(content)
	}

	switch platform {
	case payment.Stripe:
		return handleConfig("Stripe", &paymentModel.StripeConfig{})
	case payment.AlipayF2F:
		return handleConfig("Alipay", &paymentModel.AlipayF2FConfig{})
	case payment.EPay:
		return handleConfig("Epay", &paymentModel.EPayConfig{})
	default:
		return ""
	}
}
