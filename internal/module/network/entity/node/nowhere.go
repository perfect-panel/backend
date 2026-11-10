package node

import (
	"encoding/hex"
	"errors"
	"strings"
)

// NowhereSharedKey projects a subscription credential onto NW3's 32-character
// lowercase hexadecimal ASCII key. It does not change the stored UUID or the
// credentials used by other protocols. Both node delivery and subscriptions
// must use this projection.
func NowhereSharedKey(credential string) (string, error) {
	if len(credential) == 36 {
		if credential[8] != '-' || credential[13] != '-' || credential[18] != '-' || credential[23] != '-' {
			return "", errNowhereCredential
		}
		credential = credential[:8] + credential[9:13] + credential[14:18] + credential[19:23] + credential[24:]
	}
	if len(credential) != 32 {
		return "", errNowhereCredential
	}
	var value [16]byte
	if _, err := hex.Decode(value[:], []byte(credential)); err != nil {
		return "", errNowhereCredential
	}
	return hex.EncodeToString(value[:]), nil
}

var errNowhereCredential = errors.New("NW3 requires a 32-character hexadecimal key or a canonical subscription UUID")

// upgradeStoredNowhere projects pre-NW3 rows into the current protocol. It is
// deliberately limited to stored v0/v1 settings; validation of new admin
// writes only accepts NW3. Saving the projected config persists the upgrade.
func upgradeStoredNowhere(protocol *Protocol) {
	if normalizeProtocolType(protocol.Type) != "nowhere" || (protocol.Version != 0 && protocol.Version != 1) {
		return
	}
	protocol.Type = "nowhere"
	protocol.Version = 3
	protocol.ALPN = []string{"nw3"}
	if strings.TrimSpace(protocol.Obfs) == "" {
		protocol.Obfs = "morph"
	}
}
