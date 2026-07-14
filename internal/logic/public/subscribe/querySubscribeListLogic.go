package subscribe

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type QuerySubscribeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe list
func NewQuerySubscribeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *QuerySubscribeListLogic {
	return &QuerySubscribeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *QuerySubscribeListLogic) QuerySubscribeList(req *dto.QuerySubscribeListRequest) (resp *dto.QuerySubscribeListResponse, err error) {

	total, data, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
		Page:            1,
		Size:            9999,
		Language:        req.Language,
		Sell:            true,
		DefaultLanguage: true,
	})
	if err != nil {
		l.Errorw("[QuerySubscribeListLogic] Database Error", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "QuerySubscribeList error: %v", err.Error())
	}

	resp = &dto.QuerySubscribeListResponse{
		Total: total,
	}
	list := make([]dto.Subscribe, len(data))
	for i, item := range data {
		var sub dto.Subscribe
		tool.DeepCopy(&sub, item)
		if item.Discount != "" {
			var discount []dto.SubscribeDiscount
			_ = json.Unmarshal([]byte(item.Discount), &discount)
			sub.Discount = discount
			list[i] = sub
		}
		list[i] = sub
	}
	resp.List = list
	return
}
