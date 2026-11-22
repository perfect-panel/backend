package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/auth/deviceauth"
	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/infra/requestctx"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
	"github.com/perfect-panel/backend/pkg/httpx"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// DeviceMiddleware applies the device transport to the requests of devices:
// the device login, a request that declares the device login type, and any
// request a device session signed in. The signed session counts without the
// Login-Type header too, so dropping the header cannot skip the transport.
// With transport security on, such a request is accepted only in a signed,
// encrypted envelope and the data of its response is encrypted for the
// device; a response that cannot be encrypted is replaced by an error, never
// sent in plaintext.
func DeviceMiddleware(configProvider func() config.DeviceConfig, replayStore deviceauth.ReplayStore) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		cfg := configProvider()
		if !cfg.Enable {
			c.Next(ctx)
			return
		}
		loginType := string(c.GetHeader("Login-Type"))
		isDeviceLogin := string(c.Path()) == "/v1/auth/login/device"
		signedLoginType, _ := ctx.Value(requestctx.LoginType).(string)
		_, authenticated := user.FromContext(ctx)
		deviceSession := authenticated && signedLoginType == "device"
		if !isDeviceLogin && loginType != "device" && !deviceSession {
			c.Next(ctx)
			return
		}
		if !authenticated {
			ctx = context.WithValue(ctx, requestctx.LoginType, "device")
		}
		if !cfg.EnableSecurity {
			c.Next(ctx)
			return
		}
		if cfg.SecuritySecret == "" {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.SecretIsEmpty))
			c.Abort()
			return
		}
		if err := DecryptDeviceRequest(ctx, c, cfg.SecuritySecret, replayStore); err != nil {
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.InvalidCiphertext))
			c.Abort()
			return
		}
		ctx = context.WithValue(ctx, requestctx.CtxKeyDeviceSecure, true)
		c.Next(ctx)
		if err := EncryptDeviceResponse(c, cfg.SecuritySecret); err != nil {
			// Never fall back to a plaintext token or private response.
			httpx.HttpResult(c, nil, xerr.NewErrCode(xerr.ERROR))
		}
		c.Abort()
	}
}

// DecryptDeviceRequest accepts only authenticated envelopes. All validation
// completes before plaintext is installed; unsigned query parameters cannot
// be mixed into an authenticated body or accepted on a bodyless POST.
func DecryptDeviceRequest(ctx context.Context, c *app.RequestContext, secret string, replayStore deviceauth.ReplayStore) error {
	method, path := string(c.Method()), string(c.Path())
	query := c.QueryArgs()
	var envelopes []deviceauth.Envelope
	var queryParams map[string]any
	var plainBody string
	if query.Len() > 0 {
		seen := make(map[string]bool)
		valid := true
		query.VisitAll(func(key, _ []byte) {
			name := string(key)
			if (name != "data" && name != "time" && name != "sign") || seen[name] {
				valid = false
			}
			seen[name] = true
		})
		if !valid || len(seen) != 3 {
			return deviceauth.ErrInvalidEnvelope
		}
		envelope := deviceauth.Envelope{Data: string(query.Peek("data")), Time: string(query.Peek("time")), Sign: string(query.Peek("sign"))}
		plain, err := envelope.Open(secret, method, path, "query", time.Now())
		if err != nil || json.Unmarshal([]byte(plain), &queryParams) != nil || queryParams == nil {
			return deviceauth.ErrInvalidEnvelope
		}
		envelopes = append(envelopes, envelope)
	}
	if body := c.Request.Body(); len(body) > 0 {
		var envelope deviceauth.Envelope
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF {
			return deviceauth.ErrInvalidEnvelope
		}
		var err error
		plainBody, err = envelope.Open(secret, method, path, "body", time.Now())
		if err != nil || !json.Valid([]byte(plainBody)) {
			return deviceauth.ErrInvalidEnvelope
		}
		envelopes = append(envelopes, envelope)
	}
	if len(envelopes) == 0 {
		return deviceauth.ErrInvalidEnvelope
	}
	for _, envelope := range envelopes {
		if err := envelope.Consume(ctx, replayStore, secret); err != nil {
			return err
		}
	}
	query.Reset()
	for key, value := range queryParams {
		query.Set(key, fmt.Sprint(value))
	}
	c.URI().SetQueryString(string(query.QueryString()))
	if plainBody != "" {
		c.Request.SetBodyString(plainBody)
		c.Request.Header.Set("Content-Type", "application/json")
	}
	return nil
}

// EncryptDeviceResponse preserves the data/time format and adds a signature.
func EncryptDeviceResponse(c *app.RequestContext, secret string) error {
	var response map[string]any
	if err := json.Unmarshal(c.Response.Body(), &response); err != nil {
		return err
	}
	data, ok := response["data"]
	if !ok || data == nil {
		return nil
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if value, ok := data.(string); ok {
		plain = []byte(value)
	}
	ciphertext, timestamp, err := deviceauth.Encrypt(plain, secret)
	if err != nil {
		return err
	}
	response["data"] = deviceauth.Envelope{
		Data: ciphertext, Time: timestamp,
		Sign: deviceauth.Sign(secret, string(c.Method()), string(c.Path()), "response", ciphertext, timestamp),
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return err
	}
	c.Response.SetBody(encoded)
	return nil
}
