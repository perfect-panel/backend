package middleware

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/server/internal/svc"
	pkgaes "github.com/perfect-panel/server/pkg/aes"
	"github.com/perfect-panel/server/pkg/constant"
	"github.com/perfect-panel/server/pkg/result"
	"github.com/perfect-panel/server/pkg/xerr"
	"github.com/pkg/errors"
)

func DeviceMiddleware(srvCtx *svc.ServiceContext) app.HandlerFunc {
	return func(ctx context.Context, requestCtx *app.RequestContext) {
		if !srvCtx.Config.Device.Enable {
			requestCtx.Next(ctx)
			return
		}
		loginType := string(requestCtx.GetHeader("Login-Type"))
		isDeviceLoginRoute := string(requestCtx.Path()) == "/v1/auth/login/device"
		if ctx.Value(constant.CtxKeyUser) == nil && loginType != "" {
			ctx = context.WithValue(ctx, constant.LoginType, loginType)
		}
		if !isDeviceLoginRoute && loginType != "device" {
			requestCtx.Next(ctx)
			return
		}
		ctx = context.WithValue(ctx, constant.LoginType, "device")
		if !srvCtx.Config.Device.EnableSecurity {
			requestCtx.Next(ctx)
			return
		}
		if srvCtx.Config.Device.SecuritySecret == "" {
			result.HttpResult(requestCtx, nil, errors.Wrapf(xerr.NewErrCode(xerr.SecretIsEmpty), "Secret is empty"))
			requestCtx.Abort()
			return
		}

		if !DecryptDeviceRequest(requestCtx, srvCtx.Config.Device.SecuritySecret) {
			result.HttpResult(requestCtx, nil, errors.Wrapf(xerr.NewErrCode(xerr.InvalidCiphertext), "Invalid ciphertext"))
			requestCtx.Abort()
			return
		}
		ctx = context.WithValue(ctx, constant.CtxKeyDeviceSecure, true)
		requestCtx.Next(ctx)
		EncryptDeviceResponse(requestCtx, srvCtx.Config.Device.SecuritySecret)
		requestCtx.Abort()
	}
}

// DecryptDeviceRequest decrypts device-login query and request-body payloads in place.
func DecryptDeviceRequest(ctx *app.RequestContext, encryptionKey string) bool {
	query := ctx.QueryArgs()
	data := string(query.Peek("data"))
	iv := string(query.Peek("time"))
	if data != "" && iv != "" {
		plainText, err := pkgaes.Decrypt(data, encryptionKey, iv)
		if err == nil {
			params := map[string]interface{}{}
			if err := json.Unmarshal([]byte(plainText), &params); err == nil {
				for key, value := range params {
					query.Set(key, fmt.Sprint(value))
				}
				query.Del("data")
				query.Del("time")
				ctx.URI().SetQueryString(string(query.QueryString()))
			}
		}
	}

	body := ctx.Request.Body()
	if len(body) == 0 {
		return true
	}

	params := map[string]interface{}{}
	if err := json.Unmarshal(body, &params); err != nil {
		return false
	}
	data, ok := params["data"].(string)
	if !ok {
		return false
	}
	iv, ok = params["time"].(string)
	if !ok {
		return false
	}
	plainText, err := pkgaes.Decrypt(data, encryptionKey, iv)
	if err != nil {
		return false
	}
	ctx.Request.SetBody([]byte(plainText))
	return true
}

// EncryptDeviceResponse encrypts the top-level data field of a device-login response in place.
func EncryptDeviceResponse(ctx *app.RequestContext, encryptionKey string) {
	params := map[string]interface{}{}
	if err := json.Unmarshal(ctx.Response.Body(), &params); err != nil {
		return
	}
	data, ok := params["data"]
	if !ok || data == nil {
		return
	}

	plainText, err := json.Marshal(data)
	if err != nil {
		return
	}
	if stringData, ok := data.(string); ok {
		plainText = []byte(stringData)
	}
	cipherText, iv, err := pkgaes.Encrypt(plainText, encryptionKey)
	if err != nil {
		return
	}
	params["data"] = map[string]string{"data": cipherText, "time": iv}
	response, err := json.Marshal(params)
	if err != nil {
		return
	}
	ctx.Response.SetBody(response)
}
