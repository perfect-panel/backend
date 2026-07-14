package payment

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type DeletePaymentMethodLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Delete Payment Method
func NewDeletePaymentMethodLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeletePaymentMethodLogic {
	return &DeletePaymentMethodLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *DeletePaymentMethodLogic) DeletePaymentMethod(req *dto.DeletePaymentMethodRequest) error {
	if err := l.svcCtx.Store.Payment().Delete(l.ctx, req.Id); err != nil {
		l.Errorw("delete payment method error", logger.Field("id", req.Id), logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseDeletedError), "delete payment method error: %s", err.Error())
	}
	return nil
}
