package subscribe

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetSubscribeListLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get subscribe list
func NewGetSubscribeListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSubscribeListLogic {
	return &GetSubscribeListLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetSubscribeListLogic) GetSubscribeList(req *dto.GetSubscribeListRequest) (resp *dto.GetSubscribeListResponse, err error) {
	total, list, err := l.svcCtx.Store.Subscribe().FilterList(l.ctx, &subscribe.FilterParams{
		Page:     int(req.Page),
		Size:     int(req.Size),
		Language: req.Language,
		Search:   req.Search,
	})
	if err != nil {
		l.Logger.Error("[GetSubscribeListLogic] get subscribe list failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe list failed: %v", err.Error())
	}
	var (
		subscribeIdList = make([]int64, 0, len(list))
		resultList      = make([]dto.SubscribeItem, 0, len(list))
	)
	for _, item := range list {
		subscribeIdList = append(subscribeIdList, item.Id)
		var sub dto.SubscribeItem
		tool.DeepCopy(&sub, item)
		if item.Discount != "" {
			err = json.Unmarshal([]byte(item.Discount), &sub.Discount)
			if err != nil {
				l.Logger.Error("[GetSubscribeListLogic] JSON unmarshal failed: ", logger.Field("error", err.Error()), logger.Field("discount", item.Discount))
			}
		}
		sub.Nodes = dto.StringInt64Slice(tool.StringToInt64Slice(item.Nodes))
		sub.NodeTags = strings.Split(item.NodeTags, ",")
		resultList = append(resultList, sub)
	}

	subscribeMaps, err := l.svcCtx.Store.User().QueryActiveSubscriptions(l.ctx, subscribeIdList...)
	if err != nil {
		l.Logger.Error("[GetSubscribeListLogic] get user subscribe failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get user subscribe failed: %v", err.Error())
	}

	for i, item := range resultList {
		if sub, ok := subscribeMaps[item.Id]; ok {
			resultList[i].Sold = sub
		}
	}

	resp = &dto.GetSubscribeListResponse{
		Total: total,
		List:  resultList,
	}
	return
}
