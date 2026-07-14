package payment

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/report"
	paymentPlatform "github.com/perfect-panel/server/pkg/payment"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/payment"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetPaymentMethodListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetPaymentMethodListLogic Get Payment Method List
func NewGetPaymentMethodListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetPaymentMethodListLogic {
	return &GetPaymentMethodListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetPaymentMethodListLogic) GetPaymentMethodList(req *dto.GetPaymentMethodListRequest) (resp *dto.GetPaymentMethodListResponse, err error) {
	total, list, err := l.svcCtx.Store.Payment().FindListByPage(l.ctx, req.Page, req.Size, &payment.Filter{
		Search: req.Search,
		Mark:   req.Platform,
		Enable: req.Enable,
	})
	if err != nil {
		l.Errorw("find payment method list error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find payment method list error: %s", err.Error())
	}
	resp = &dto.GetPaymentMethodListResponse{
		Total: total,
		List:  make([]dto.PaymentMethodDetail, len(list)),
	}

	// gateway mod

	isGatewayMod := report.IsGatewayMode()

	for i, v := range list {
		config := make(map[string]interface{})
		_ = json.Unmarshal([]byte(v.Config), &config)
		notifyUrl := ""

		if paymentPlatform.ParsePlatform(v.Platform) != paymentPlatform.Balance {
			notifyUrl = v.Domain
			if v.Domain != "" {
				notifyUrl = strings.TrimSuffix(notifyUrl, "/")
				if isGatewayMod {
					notifyUrl += "/api/v1/notify/" + v.Platform + "/" + v.Token
				} else {
					notifyUrl += "/v1/notify/" + v.Platform + "/" + v.Token
				}
			} else {
				notifyUrl += "https://" + l.svcCtx.Config.Host
				if isGatewayMod {
					notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/api/v1/notify/" + v.Platform + "/" + v.Token
				} else {
					notifyUrl = strings.TrimSuffix(notifyUrl, "/") + "/v1/notify/" + v.Platform + "/" + v.Token
				}
			}
		}
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
			Enable:      *v.Enable,
			NotifyURL:   notifyUrl,
			Description: v.Description,
		}
	}
	return
}
