package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/perfect-panel/server/internal/model/dto"
	"github.com/perfect-panel/server/pkg/logger"
)

type AppleLoginCallbackLogic struct {
	logger.Logger
	ctx  context.Context
	deps AppleLoginCallbackDependencies
}

type AppleLoginRedirect struct {
	StatusCode int
	Location   string
}

// Apple Login Callback
func NewAppleLoginCallbackLogic(ctx context.Context, deps AppleLoginCallbackDependencies) *AppleLoginCallbackLogic {
	return &AppleLoginCallbackLogic{
		Logger: logger.WithContext(ctx),
		ctx:    ctx,
		deps:   deps,
	}
}

func (l *AppleLoginCallbackLogic) AppleLoginCallback(req *dto.AppleLoginCallbackRequest) (*AppleLoginRedirect, error) {
	// validate the state code
	result, err := l.deps.Redis.Get(l.ctx, fmt.Sprintf("apple:%s", req.State)).Result()
	if err != nil {
		l.Errorw("get apple state code from redis failed", logger.Field("error", err.Error()), logger.Field("code", req.State))
		return appleLoginRedirect(l.deps.FallbackRedirect, req, http.StatusTemporaryRedirect), nil
	}
	redirect := appleLoginRedirect(result, req, http.StatusFound)
	l.Infow("redirect to apple login page", logger.Field("url", redirect.Location))
	return redirect, nil
}

func appleLoginRedirect(location string, req *dto.AppleLoginCallbackRequest, statusCode int) *AppleLoginRedirect {
	if statusCode == http.StatusTemporaryRedirect {
		return &AppleLoginRedirect{StatusCode: statusCode, Location: location}
	}

	parsedLocation, err := url.Parse(location)
	if err != nil {
		return &AppleLoginRedirect{StatusCode: statusCode, Location: location}
	}

	query := parsedLocation.Query()
	query.Set("method", "apple")
	query.Set("code", req.Code)
	query.Set("state", req.State)
	parsedLocation.RawQuery = query.Encode()

	return &AppleLoginRedirect{
		StatusCode: statusCode,
		Location:   parsedLocation.String(),
	}
}
