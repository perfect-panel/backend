package initialize

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/model/entity/auth"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/logger"
	"github.com/perfect-panel/server/pkg/tool"
)

// Email get email smtp config
func Email(ctx *svc.ServiceContext) {
	logger.Debug("Email config initialization")
	method, err := ctx.Store.Auth().FindOneByMethod(context.Background(), "email")
	if err != nil {
		panic(fmt.Sprintf("[Error] Initialization Failed to find email auth method: %v", err.Error()))
	}
	var cfg config.EmailConfig
	var emailConfig = new(auth.EmailAuthConfig)
	emailConfig.Unmarshal(method.Config)
	tool.DeepCopy(&cfg, emailConfig)
	cfg.Enable = *method.Enabled
	value, _ := json.Marshal(emailConfig.PlatformConfig)
	cfg.PlatformConfig = string(value)
	ctx.Config.Email = cfg
}
