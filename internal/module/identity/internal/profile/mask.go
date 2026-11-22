package profile

import (
	"strings"

	"github.com/perfect-panel/backend/internal/auth/identifier"
	dto "github.com/perfect-panel/backend/internal/module/identity/contract"
	"github.com/perfect-panel/backend/internal/module/identity/entity/user"
)

// The self-service views show an account its own identifiers masked. An
// email address is the account's own sign-in name and stays readable; a
// phone number keeps its shape; a provider subject is a lookup key at the
// provider; a device identifier is a bearer credential
// (docs/design/device-authentication.md): whoever reads it from a web
// session, a screenshot or a log can sign in as the device, so it is masked
// harder than the others.

// maskAuthMethods masks the identifiers of an account's bindings for its own
// view: the phone number, the provider subjects and the device identifiers;
// the email address stays as it is.
func maskAuthMethods(methods []*user.AuthMethods) []dto.UserAuthMethod {
	list := make([]dto.UserAuthMethod, 0, len(methods))
	for _, method := range methods {
		if method == nil {
			continue
		}
		list = append(list, maskAuthMethod(dto.UserAuthMethod{
			AuthType:       method.AuthType,
			AuthIdentifier: method.AuthIdentifier,
			Verified:       method.Verified,
		}))
	}
	return list
}

// maskAuthMethod masks one binding's identifier by its type.
func maskAuthMethod(method dto.UserAuthMethod) dto.UserAuthMethod {
	switch method.AuthType {
	case identifier.Email:
	case identifier.Mobile:
		method.AuthIdentifier = identifier.MaskPhoneNumber(method.AuthIdentifier)
	case identifier.Device:
		method.AuthIdentifier = maskDeviceIdentifier(method.AuthIdentifier)
	default:
		method.AuthIdentifier = maskOpenID(method.AuthIdentifier)
	}
	return method
}

// maskDevices masks the identifiers of an account's devices for its own view.
func maskDevices(devices []dto.UserDevice) []dto.UserDevice {
	for i := range devices {
		devices[i].Identifier = maskDeviceIdentifier(devices[i].Identifier)
	}
	return devices
}

// maskDeviceIdentifier keeps the first four characters of a device
// identifier, enough to tell devices apart in a list, and hides the rest;
// one of twelve characters or fewer is hidden entirely.
func maskDeviceIdentifier(id string) string {
	runes := []rune(id)
	if len(runes) <= 12 {
		return "***"
	}
	return string(runes[:4]) + strings.Repeat("*", 8)
}

// maskOpenID masks a provider identifier, keeping its first and last three
// characters; one of six characters or fewer is masked entirely.
func maskOpenID(openID string) string {
	length := len(openID)
	if length <= 6 {
		return "***"
	}

	maskLength := length - 6
	mask := make([]byte, maskLength)
	for i := range mask {
		mask[i] = '*'
	}
	return openID[:3] + string(mask) + openID[length-3:]
}
