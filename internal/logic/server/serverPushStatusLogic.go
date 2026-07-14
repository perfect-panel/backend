package server

import (
	"context"
	"errors"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/timeutil"
)

type ServerPushStatusLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewServerPushStatusLogic Push server status
func NewServerPushStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ServerPushStatusLogic {
	return &ServerPushStatusLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ServerPushStatusLogic) ServerPushStatus(req *dto.ServerPushStatusRequest) error {
	// Find server info
	serverInfo, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil || serverInfo.Id <= 0 {
		l.Errorw("[PushOnlineUsers] FindOne error", logger.Field("error", err))
		return errors.New("server not found")
	}
	err = l.svcCtx.Store.Node().UpdateStatusCache(l.ctx, req.ServerId, &node.Status{
		Cpu:       req.Cpu,
		Mem:       req.Mem,
		Disk:      req.Disk,
		UpdatedAt: req.UpdatedAt,
	})
	if err != nil {
		l.Errorw("[ServerPushStatus] UpdateNodeStatus error", logger.Field("error", err))
		return errors.New("update node status failed")
	}
	now := timeutil.Now()
	serverInfo.LastReportedAt = &now

	err = l.svcCtx.Store.Node().UpdateServer(l.ctx, serverInfo)
	if err != nil {
		l.Errorw("[ServerPushStatus] UpdateServer error", logger.Field("error", err))
		return nil
	}

	return nil
}
