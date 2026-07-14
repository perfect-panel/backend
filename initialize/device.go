package initialize

import (
	"context"

	"github.com/perfect-panel/server/pkg/logger"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/tool"
)

func Device(ctx *svc.ServiceContext) {
	logger.Debug("device config initialization")
	method, err := ctx.Store.Auth().FindOneByMethod(context.Background(), "device")
	if err != nil {
		panic(err)
	}
	var cfg config.DeviceConfig
	var deviceConfig auth.DeviceConfig
	deviceConfig.Unmarshal(method.Config)
	tool.DeepCopy(&cfg, deviceConfig)
	cfg.Enable = *method.Enabled
	ctx.Config.Device = cfg
}
