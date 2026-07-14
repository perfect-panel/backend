package initialize

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/server/pkg/logger"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/entity/system"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/nodeMultiplier"
	"github.com/perfect-panel/server/pkg/tool"
)

func Node(ctx *svc.ServiceContext) {
	logger.Debug("Node config initialization")
	configs, err := ctx.Store.System().GetNodeConfig(context.Background())
	if err != nil {
		panic(err)
	}
	var nodeConfig config.NodeDBConfig
	tool.SystemConfigSliceReflectToStruct(configs, &nodeConfig)
	c := config.NodeConfig{
		NodeSecret:             nodeConfig.NodeSecret,
		NodePullInterval:       nodeConfig.NodePullInterval,
		NodePushInterval:       nodeConfig.NodePushInterval,
		IPStrategy:             nodeConfig.IPStrategy,
		TrafficReportThreshold: nodeConfig.TrafficReportThreshold,
	}
	if nodeConfig.DNS != "" {
		var dns []config.NodeDNS
		err = json.Unmarshal([]byte(nodeConfig.DNS), &dns)
		if err != nil {
			logger.Errorf("[Node] Unmarshal DNS config error: %s", err.Error())
			panic(err)
		}
		c.DNS = dns
	}
	if nodeConfig.Block != "" {
		var block []string
		_ = json.Unmarshal([]byte(nodeConfig.Block), &block)
		c.Block = tool.RemoveDuplicateElements(block...)
	}
	if nodeConfig.Outbound != "" {
		var outbound []config.NodeOutbound
		err = json.Unmarshal([]byte(nodeConfig.Outbound), &outbound)
		if err != nil {
			logger.Errorf("[Node] Unmarshal Outbound config error: %s", err.Error())
			panic(err)
		}
		c.Outbound = outbound
	}

	ctx.Config.Node = c

	nodeMultiplierData, err := ctx.Store.System().FindNodeMultiplierConfig(context.Background())
	if err != nil {
		logger.Error("Get Node Multiplier Config Error: ", logger.Field("error", err.Error()))
		return
	}

	// Manager initialization
	if nodeMultiplierData.Id == 0 {
		if err := ctx.Store.System().Insert(context.Background(), &system.System{
			Key:      "NodeMultiplierConfig",
			Value:    "[]",
			Type:     "string",
			Desc:     "Node Multiplier Config",
			Category: "server",
		}); err != nil {
			logger.Errorf("Create Node Multiplier Config Error: %s", err.Error())
		}
		return
	}

	var periods []nodeMultiplier.TimePeriod
	if err := json.Unmarshal([]byte(nodeMultiplierData.Value), &periods); err != nil {
		logger.Error("Unmarshal Node Multiplier Config Error: ", logger.Field("error", err.Error()), logger.Field("value", nodeMultiplierData.Value))
	}
	ctx.NodeMultiplierManager = nodeMultiplier.NewNodeMultiplierManager(periods)
}
