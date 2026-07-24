package plan

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetSubscribeDetailsLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Get subscribe details
func newGetSubscribeDetailsLogic(ctx context.Context, deps Deps) *GetSubscribeDetailsLogic {
	return &GetSubscribeDetailsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *GetSubscribeDetailsLogic) GetSubscribeDetails(req *dto.GetSubscribeDetailsRequest) (resp *dto.Subscribe, err error) {
	sub, err := l.deps.Plans.FindOne(l.ctx, req.Id)
	if err != nil {
		l.Logger.Error("[GetSubscribeDetailsLogic] get subscribe details failed: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "get subscribe details failed: %v", err.Error())
	}
	resp = &dto.Subscribe{}
	tool.DeepCopy(resp, sub)
	if sub.Discount != "" {
		err = json.Unmarshal([]byte(sub.Discount), &resp.Discount)
		if err != nil {
			l.Logger.Error("[GetSubscribeDetailsLogic] JSON unmarshal failed: ", logger.Field("error", err.Error()), logger.Field("discount", sub.Discount))
		}
	}
	resp.Nodes = dto.StringInt64Slice(tool.StringToInt64Slice(sub.Nodes))
	resp.NodeTags = strings.Split(sub.NodeTags, ",")
	return resp, nil
}
