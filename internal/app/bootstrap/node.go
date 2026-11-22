package bootstrap

import (
	"context"
	"encoding/json"

	"github.com/perfect-panel/backend/internal/config"
	"github.com/perfect-panel/backend/internal/module/network"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/logger"
	"github.com/perfect-panel/backend/pkg/random"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// nodeMultiplierKey is the server setting holding the traffic multiplier
// periods. It is read on its own, not through config.NodeDBConfig.
const nodeMultiplierKey = "NodeMultiplierConfig"

// Node loads the node settings and the traffic multiplier. Everything is read
// before anything is published, so a failed load keeps the previous node
// configuration and multiplier manager.
func Node(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Node config initialization")
	rows, err := deps.Settings.GetNodeConfig(ctx)
	if err != nil {
		return wrapf(err, xerr.DatabaseQueryError, "read %s settings", categoryNode)
	}
	// The admin settings read parses the same rows the same way.
	c, err := system.ParseNodeConfig(rows)
	if err != nil {
		return wrapf(err, xerr.ERROR, "decode the node settings")
	}

	nodeMultiplierData, err := deps.Settings.FindNodeMultiplierConfig(ctx)
	if err != nil {
		return wrapf(err, xerr.DatabaseQueryError, "read the %s setting", nodeMultiplierKey)
	}
	if nodeMultiplierData == nil || nodeMultiplierData.Id == 0 {
		// First start: seed an empty multiplier table and keep the manager
		// the process already has.
		if err := deps.Settings.Insert(ctx, &system.System{
			Key:      nodeMultiplierKey,
			Value:    "[]",
			Type:     "string",
			Desc:     "Node Multiplier Config",
			Category: categoryNode,
		}); err != nil {
			return wrapf(err, xerr.DatabaseInsertError, "create the %s setting", nodeMultiplierKey)
		}
		deps.updateRuntime(func(current *config.Runtime) { current.Node = c })
		return nil
	}

	var periods []network.MultiplierPeriod
	if err := json.Unmarshal([]byte(nodeMultiplierData.Value), &periods); err != nil {
		// Kept lenient as before: unreadable periods apply no multiplier.
		logger.WithContext(ctx).Errorw("[Node] the node multiplier setting is not valid JSON, applying no multiplier",
			logger.Field("key", nodeMultiplierKey), logger.Field("error", err.Error()), logger.Field("value", nodeMultiplierData.Value))
	}
	deps.updateRuntime(func(current *config.Runtime) { current.Node = c })
	if deps.SetNodeMultiplierManager != nil {
		deps.SetNodeMultiplierManager(network.NewMultiplierManager(periods))
	}
	return nil
}

// LegacyDefaultNodeSecret is the node secret the original seed data shipped. The
// value is public knowledge, so an installation still carrying it serves the
// node API — which hands out every user's subscription uuid — to anyone.
const LegacyDefaultNodeSecret = "12345678"

// nodeSecretLength yields about 190 bits over the 62-character alphabet.
const nodeSecretLength = 32

// NodeSecret provisions a random node secret when the database does not carry
// one yet, so a fresh installation never serves the node API with a guessable
// credential. It has to run after Migrate, which seeds the empty row, and
// before Node, which copies the value into the runtime config.
//
// An installation still holding LegacyDefaultNodeSecret is only reported, never
// rotated: every node was configured with that secret, so rotating it here would
// silently cut them off. The operator rotates it from the admin panel and
// reconfigures the nodes in the same window.
func NodeSecret(ctx context.Context, deps *Dependencies) error {
	logger.Debug("Node secret initialization")
	// The read and the write share a transaction so the read goes to the
	// database instead of Redis; GetNodeConfig is a cached query, and the write
	// below does not invalidate that cache.
	err := deps.SettingsTx.InSettingsTx(ctx, func(settings NodeSettings) error {
		configs, err := settings.GetNodeConfig(ctx)
		if err != nil {
			return wrapf(err, xerr.DatabaseQueryError, "read %s settings", categoryNode)
		}

		// The raw value decides, whatever type the row declares: a stored
		// secret that does not decode is still a secret every node uses, and
		// replacing it would cut them all off.
		switch storedSettingValue(configs, nodeSecretKey) {
		case "":
			secret := random.KeyNew(nodeSecretLength, 1)
			if err := settings.UpdateValueByCategoryKey(ctx, categoryNode, nodeSecretKey, secret); err != nil {
				return wrapf(err, xerr.DatabaseUpdateError, "store the generated node secret")
			}
			logger.WithContext(ctx).Info("[NodeSecret] generated a random node secret, read it from the admin panel to configure nodes")
		case LegacyDefaultNodeSecret:
			logger.WithContext(ctx).Error("[NodeSecret] the node secret is still the well-known default, rotate it from the admin panel and reconfigure every node")
		}
		return nil
	})
	if err != nil {
		logger.WithContext(ctx).Errorf("[NodeSecret] provision error: %v", err.Error())
		return err
	}
	return nil
}

// nodeSecretKey is the server setting holding the node API credential.
const nodeSecretKey = "NodeSecret"

// storedSettingValue returns the value stored under key; like the decoder,
// the last of duplicate rows wins.
func storedSettingValue(entries []*system.System, key string) string {
	value := ""
	for _, entry := range entries {
		if entry != nil && entry.Key == key {
			value = entry.Value
		}
	}
	return value
}
