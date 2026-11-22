package systemsetting

import (
	"errors"

	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// The admin settings API shows a stored secret as dto.SecretMask and keeps
// the stored value when an update carries the mask back, so an
// administrator who never learns the secret can still edit the settings
// around it. The node secret is the one credential the API shows in clear:
// administrators copy it into every node's configuration to deploy a node
// (see nodeConfigView).

// maskSecret shows a stored secret masked; an unset secret stays empty, so
// the admin panel can tell "not configured" from "configured".
func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	return dto.SecretMask
}

// errMaskedSecretUnknown refuses an update that carries the mask for a
// secret the store has no value of, which the mask cannot stand in for.
var errMaskedSecretUnknown = errors.New("masked secret has no stored value")

func maskedSecretUnknown(what string) error {
	return xerr.Wrapf(errMaskedSecretUnknown, xerr.InvalidParams, "%s is masked but no stored value exists to keep", what)
}

// maskVerifySecrets masks the Turnstile secret of the verification
// settings.
func maskVerifySecrets(c *dto.VerifyConfig) {
	c.TurnstileSecret = maskSecret(c.TurnstileSecret)
}

// keepVerifySecrets resolves a masked Turnstile secret from the stored
// settings; nil stored settings are unknown.
func keepVerifySecrets(req *dto.VerifyConfig, stored *dto.VerifyConfig) error {
	if req.TurnstileSecret != dto.SecretMask {
		return nil
	}
	if stored == nil || stored.TurnstileSecret == "" {
		return maskedSecretUnknown("turnstile_secret")
	}
	req.TurnstileSecret = stored.TurnstileSecret
	return nil
}

// maskCurrencySecrets masks the exchange-rate provider's access key.
func maskCurrencySecrets(c *dto.CurrencyConfig) {
	c.AccessKey = maskSecret(c.AccessKey)
}

// keepCurrencySecrets resolves a masked access key from the stored settings.
func keepCurrencySecrets(req *dto.CurrencyConfig, stored *dto.CurrencyConfig) error {
	if req.AccessKey != dto.SecretMask {
		return nil
	}
	if stored == nil || stored.AccessKey == "" {
		return maskedSecretUnknown("access_key")
	}
	req.AccessKey = stored.AccessKey
	return nil
}

// maskOutboundSecrets masks the credentials of every node outbound: its
// password, UUID and encryption password. The Reality public key is public
// by name and the node secret stays readable (see the package comment
// above).
func maskOutboundSecrets(outbounds []dto.PlatformNodeOutboundSnapshot) {
	for i := range outbounds {
		outbounds[i].Password = maskSecret(outbounds[i].Password)
		outbounds[i].UUID = maskSecret(outbounds[i].UUID)
		outbounds[i].EncryptionPassword = maskSecret(outbounds[i].EncryptionPassword)
	}
}

// keepOutboundSecrets resolves the masked credentials of the requested
// outbounds from the stored ones. A requested outbound is matched to the
// stored outbound of the same name, or, when no stored outbound has that
// name, to the one at the same position, so a renamed outbound keeps its
// credentials and a reordered one does not take another's.
func keepOutboundSecrets(requested, stored []dto.PlatformNodeOutboundSnapshot) error {
	byName := make(map[string]int, len(stored))
	for i, outbound := range stored {
		if _, taken := byName[outbound.Name]; !taken {
			byName[outbound.Name] = i
		}
	}
	for i := range requested {
		outbound := &requested[i]
		if outbound.Password != dto.SecretMask && outbound.UUID != dto.SecretMask && outbound.EncryptionPassword != dto.SecretMask {
			continue
		}
		previous, ok := matchOutbound(outbound.Name, i, byName, stored)
		if !ok {
			return maskedSecretUnknown("outbound " + outbound.Name)
		}
		for _, secret := range []struct {
			field  *string
			stored string
			name   string
		}{
			{&outbound.Password, previous.Password, "password"},
			{&outbound.UUID, previous.UUID, "uuid"},
			{&outbound.EncryptionPassword, previous.EncryptionPassword, "encryption_password"},
		} {
			if *secret.field != dto.SecretMask {
				continue
			}
			if secret.stored == "" {
				return maskedSecretUnknown("outbound " + outbound.Name + " " + secret.name)
			}
			*secret.field = secret.stored
		}
	}
	return nil
}

func matchOutbound(name string, position int, byName map[string]int, stored []dto.PlatformNodeOutboundSnapshot) (dto.PlatformNodeOutboundSnapshot, bool) {
	if i, ok := byName[name]; ok {
		return stored[i], true
	}
	if position < len(stored) {
		return stored[position], true
	}
	return dto.PlatformNodeOutboundSnapshot{}, false
}
