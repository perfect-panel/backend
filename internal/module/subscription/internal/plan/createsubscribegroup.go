package plan

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/subscribe"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type CreateSubscribeGroupLogic struct {
	logger.Logger
	ctx  context.Context
	deps Deps
}

// Create subscribe group
func newCreateSubscribeGroupLogic(ctx context.Context, deps Deps) *CreateSubscribeGroupLogic {
	return &CreateSubscribeGroupLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *CreateSubscribeGroupLogic) CreateSubscribeGroup(req *dto.CreateSubscribeGroupRequest) error {
	err := l.deps.Plans.CreateGroup(l.ctx, &subscribe.Group{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		l.Logger.Error("[CreateSubscribeGroupLogic] create subscribe group failed: ", logger.Field("error", err.Error()))
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create subscribe group failed: %v", err.Error())
	}
	return nil
}
