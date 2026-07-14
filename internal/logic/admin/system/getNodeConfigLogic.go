package system

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

type GetNodeConfigLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetNodeConfigLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetNodeConfigLogic {
	return &GetNodeConfigLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetNodeConfigLogic) GetNodeConfig() (*dto.NodeConfig, error) {
	// get server config from db
	configs, err := l.svcCtx.Store.System().GetNodeConfig(l.ctx)
	if err != nil {
		l.Errorw("[GetNodeConfigLogic] GetNodeConfig get server config error: ", logger.Field("error", err.Error()))
		return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "GetNodeConfig get server config error: %v", err.Error())
	}
	var dbConfig config.NodeDBConfig
	tool.SystemConfigSliceReflectToStruct(configs, &dbConfig)
	c := &dto.NodeConfig{
		NodeSecret:             dbConfig.NodeSecret,
		NodePullInterval:       dbConfig.NodePullInterval,
		NodePushInterval:       dbConfig.NodePushInterval,
		IPStrategy:             dbConfig.IPStrategy,
		TrafficReportThreshold: dbConfig.TrafficReportThreshold,
	}

	if dbConfig.DNS != "" {
		var dns []dto.NodeDNS
		err = json.Unmarshal([]byte(dbConfig.DNS), &dns)
		if err != nil {
			logger.Errorf("[Node] Unmarshal DNS config error: %s", err.Error())
			panic(err)
		}
		c.DNS = dns
	}
	if dbConfig.Block != "" {
		var block []string
		_ = json.Unmarshal([]byte(dbConfig.Block), &block)
		c.Block = tool.RemoveDuplicateElements(block...)
	}
	if dbConfig.Outbound != "" {
		var outbound []dto.NodeOutbound
		err = json.Unmarshal([]byte(dbConfig.Outbound), &outbound)
		if err != nil {
			logger.Errorf("[Node] Unmarshal Outbound config error: %s", err.Error())
			panic(err)
		}
		c.Outbound = outbound
	}

	return c, nil
}
