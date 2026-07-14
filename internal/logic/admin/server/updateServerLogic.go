package server

import (
	"context"
	"strings"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/model/entity/node"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/ip"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type UpdateServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewUpdateServerLogic Update Server
func NewUpdateServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateServerLogic {
	return &UpdateServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UpdateServerLogic) UpdateServer(req *dto.UpdateServerRequest) error {
	nodeStore := l.svcCtx.Store.Node()
	data, err := nodeStore.FindOneServer(l.ctx, req.Id)
	if err != nil {
		l.Errorf("[UpdateServer] FindOneServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "find server error: %v", err.Error())
	}
	data.Name = req.Name
	data.Country = req.Country
	data.City = req.City
	// only update address when it's  different
	if req.Address != data.Address {
		// query server ip location
		result, err := ip.GetRegionByIp(req.Address)
		if err != nil {
			l.Errorf("[UpdateServer] GetRegionByIp Error: %v", err.Error())
		} else {
			data.City = result.City
			data.Country = result.Country
		}
		// update address
		data.Address = req.Address
	}
	protocols := make([]node.Protocol, 0)
	for _, item := range req.Protocols {
		if item.Type == "" {
			return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols type is empty"), "protocols type is empty")
		}
		var protocol node.Protocol
		tool.DeepCopy(&protocol, item)

		// VLESS Reality Key Generation
		if protocol.Type == "vless" {
			if protocol.Security == "reality" {
				if protocol.RealityPublicKey == "" {
					public, private, err := tool.Curve25519Genkey(false, "")
					if err != nil {
						l.Errorf("[CreateServer] Generate Reality Key Error: %v", err.Error())
						return errors.Wrapf(xerr.NewErrCode(xerr.ERROR), "generate reality key error: %v", err)
					}
					protocol.RealityPublicKey = public
					protocol.RealityPrivateKey = private
					protocol.RealityShortId = tool.GenerateShortID(private)
				}
				if protocol.RealityServerAddr == "" {
					protocol.RealityServerAddr = protocol.SNI
				}
				if protocol.RealityServerPort == 0 {
					protocol.RealityServerPort = 443
				}
			}

		}
		// ShadowSocks 2022 Key Generation
		if protocol.Type == "shadowsocks" {
			if strings.Contains(protocol.Cipher, "2022") {
				var length int
				switch protocol.Cipher {
				case "2022-blake3-aes-128-gcm":
					length = 16
				default:
					length = 32
				}
				if len(protocol.ServerKey) != length {
					protocol.ServerKey = tool.GenerateCipher(protocol.ServerKey, length)
				}
			}
		}
		protocols = append(protocols, protocol)
	}
	err = data.MarshalProtocols(protocols)
	if err != nil {
		l.Errorf("[UpdateServer] Marshal Protocols Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols marshal error"), "protocols marshal error: %v", err)
	}

	err = nodeStore.UpdateServer(l.ctx, data)
	if err != nil {
		l.Errorf("[UpdateServer] UpdateServer Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update server error: %v", err.Error())
	}

	return nodeStore.ClearNodeCache(l.ctx, &node.FilterNodeParams{
		Page:     1,
		Size:     1000,
		ServerId: []int64{req.Id},
		Search:   "",
	})
}
