package constant

type CtxKey string

const (
	CtxKeyUser         CtxKey = "user"
	CtxKeySessionID    CtxKey = "sessionId"
	CtxKeyRequestHost  CtxKey = "requestHost"
	CtxKeyPlatform     CtxKey = "platform"
	CtxKeyPayment      CtxKey = "payment"
	CtxKeyDeviceSecure CtxKey = "deviceSecure"
	CtxKeyClientIP     CtxKey = "clientIP"
	LoginType          CtxKey = "loginType"
)
