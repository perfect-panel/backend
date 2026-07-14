package server

import (
	"context"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetServerProtocolsLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// Get Server Protocols
func NewGetServerProtocolsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetServerProtocolsLogic {
	return &GetServerProtocolsLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetServerProtocolsLogic) GetServerProtocols(req *dto.GetServerProtocolsRequest) (resp *dto.GetServerProtocolsResponse, err error) {
	// find server
	data, err := l.svcCtx.Store.Node().FindOneServer(l.ctx, req.Id)
	if err != nil {
		l.Errorf("[GetServerProtocols] FindOneServer Error: %s", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[GetServerProtocols] FindOneServer Error: %s", err.Error())
	}

	// handler protocols
	var protocols []dto.Protocol
	dst, err := data.UnmarshalProtocols()
	if err != nil {
		l.Errorf("[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "[FilterServerList] UnmarshalProtocols Error: %s", err.Error())
	}
	tool.DeepCopy(&protocols, dst)

	return &dto.GetServerProtocolsResponse{
		Protocols: protocols,
	}, nil
}
