package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
)

type PushOnlineUsersLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewPushOnlineUsersLogic Push online users
func NewPushOnlineUsersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PushOnlineUsersLogic {
	return &PushOnlineUsersLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PushOnlineUsersLogic) PushOnlineUsers(req *dto.OnlineUsersRequest) error {
	// 验证请求数据
	if req.ServerId <= 0 || len(req.Users) == 0 {
		return errors.New("invalid request parameters")
	}

	// 验证用户数据
	for _, user := range req.Users {
		if user.SID <= 0 || user.IP == "" {
			return fmt.Errorf("invalid user data: uid=%d, ip=%s", user.SID, user.IP)
		}
	}

	// Find server info
	_, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.ServerId)
	if err != nil {
		l.Errorw("[PushOnlineUsers] FindOne error", logger.Field("error", err))
		return fmt.Errorf("server not found: %w", err)
	}

	onlineUsers := make(node.OnlineUserSubscribe)
	for _, user := range req.Users {
		if online, ok := onlineUsers[user.SID]; ok {
			// If user already exists, update IP if different
			online = append(online, user.IP)
			onlineUsers[user.SID] = online
		} else {
			// New user, add to map
			onlineUsers[user.SID] = []string{user.IP}
		}
	}
	err = l.svcCtx.Store.Node().UpdateOnlineUserSubscribe(l.ctx, req.ServerId, req.Protocol, onlineUsers)
	if err != nil {
		l.Errorw("[PushOnlineUsers] cache operation error", logger.Field("error", err))
		return err
	}

	err = l.svcCtx.Store.Node().UpdateOnlineUserSubscribeGlobal(l.ctx, onlineUsers)

	if err != nil {
		l.Errorw("[PushOnlineUsers] cache operation error", logger.Field("error", err))
		return err
	}

	return nil
}
