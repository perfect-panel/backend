package tool

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/logger"
)

type GetVersionLogic struct {
	logger.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

// NewGetVersionLogic Get Version
func NewGetVersionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetVersionLogic {
	return &GetVersionLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetVersionLogic) GetVersion() (resp *dto.VersionResponse, err error) {
	version := constant.Version
	buildTime := constant.BuildTime

	// Normalize unknown values
	if version == "unknown version" {
		version = "unknown"
	}
	if buildTime == "unknown time" {
		buildTime = "unknown"
	}

	// Format version based on whether it starts with 'v'
	var formattedVersion string
	if len(version) > 0 && version[0] == 'v' {
		formattedVersion = fmt.Sprintf("%s(%s)", version[1:], buildTime)
	} else {
		formattedVersion = fmt.Sprintf("%s(%s) Develop", version, buildTime)
	}

	return &dto.VersionResponse{
		Version: formattedVersion,
	}, nil
}
