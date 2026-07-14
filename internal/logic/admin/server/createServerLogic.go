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

type CreateServerLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewCreateServerLogic Create Server
func NewCreateServerLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateServerLogic {
	return &CreateServerLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *CreateServerLogic) CreateServer(req *dto.CreateServerRequest) error {
	data := node.Server{
		Name:      req.Name,
		Country:   req.Country,
		City:      req.City,
		Address:   req.Address,
		Sort:      req.Sort,
		Protocols: "",
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

	err := data.MarshalProtocols(protocols)
	if err != nil {
		l.Errorf("[CreateServer] Marshal Protocols Error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCodeMsg(xerr.InvalidParams, "protocols marshal error"), "protocols marshal error: %v", err)
	}
	if data.City == "" && data.Country == "" {
		// query server ip location
		result, err := ip.GetRegionByIp(req.Address)
		if err != nil {
			l.Errorf("[CreateServer] GetRegionByIp Error: %v", err.Error())
		} else {
			data.City = result.City
			data.Country = result.Country
		}
	}
	err = l.svcCtx.Store.Node().InsertServer(l.ctx, &data)
	if err != nil {
		l.Errorf("[CreateServer] Insert Server error: %v", err.Error())
		return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "insert server error: %v", err)
	}
	return nil
}
