package systemsetting

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/perfect-panel/backend/internal/config"
	dto "github.com/perfect-panel/backend/internal/module/platform/contract"
	"github.com/perfect-panel/backend/internal/module/platform/entity/log"
	"github.com/perfect-panel/backend/internal/module/platform/entity/system"
	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/perfect-panel/backend/pkg/xerr"
)

// adminContext is an administrator's request: address, agent and account 9.
func adminContext() context.Context {
	return requestmeta.WithActor(requestmeta.With(context.Background(), requestmeta.New("203.0.113.9", "AdminPanel/1.0")), 9)
}

// actions reads the administrator actions recorded so far, oldest first.
func (w *settingsWorld) actions(t *testing.T) []log.AdminAction {
	t.Helper()
	var rows []log.SystemLog
	if err := w.db.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	actions := make([]log.AdminAction, 0, len(rows))
	for _, row := range rows {
		if row.Type != log.TypeAdminAction.Uint8() || row.ObjectID != 9 {
			t.Fatalf("row %+v is not an action filed under administrator 9", row)
		}
		var action log.AdminAction
		if err := action.Unmarshal([]byte(row.Content)); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	return actions
}

// storedValue reads a setting as stored, in clear.
func (w *settingsWorld) storedValue(t *testing.T, category, key string) string {
	t.Helper()
	var row system.System
	if err := w.db.Where("category = ? AND `key` = ?", category, key).First(&row).Error; err != nil {
		t.Fatalf("%s.%s: %v", category, key, err)
	}
	return row.Value
}

// The secrets read back masked; an update that carries the mask keeps the
// stored value, an update that carries a value replaces it and an empty
// field clears it. Unset secrets read back empty, so the panel can tell
// "not configured" from "configured".
func TestSecretsReadBackMaskedAndAreKeptWhenMasked(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := adminContext()

	if got, err := w.svc.GetVerifyConfig(ctx); err != nil || got.TurnstileSecret != "" {
		t.Fatalf("unset secret reads as %q (err %v), want empty", got.TurnstileSecret, err)
	}
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site", TurnstileSecret: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.svc.GetVerifyConfig(ctx); got.TurnstileSecret != dto.SecretMask || got.TurnstileSiteKey != "site" {
		t.Fatalf("verify = %+v, want the secret masked and the site key in clear", got)
	}
	// The panel sends the settings back as it received them.
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site-2", TurnstileSecret: dto.SecretMask, EnableLoginVerify: true}); err != nil {
		t.Fatal(err)
	}
	if got := w.storedValue(t, "verify", "TurnstileSecret"); got != "s3cret" {
		t.Fatalf("stored secret = %q, want the masked update to keep it", got)
	}
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site-2", TurnstileSecret: "n3w"}); err != nil {
		t.Fatal(err)
	}
	if got := w.storedValue(t, "verify", "TurnstileSecret"); got != "n3w" {
		t.Fatalf("stored secret = %q, want it replaced", got)
	}
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site-2"}); err != nil {
		t.Fatal(err)
	}
	if got := w.storedValue(t, "verify", "TurnstileSecret"); got != "" {
		t.Fatalf("stored secret = %q, want it cleared", got)
	}

	if err := w.svc.UpdateCurrencyConfig(ctx, &dto.CurrencyConfig{AccessKey: "ak", CurrencyUnit: "USD", CurrencySymbol: "$"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.svc.GetCurrencyConfig(ctx); got.AccessKey != dto.SecretMask {
		t.Fatalf("currency = %+v, want the access key masked", got)
	}
	if err := w.svc.UpdateCurrencyConfig(ctx, &dto.CurrencyConfig{AccessKey: dto.SecretMask, CurrencyUnit: "EUR", CurrencySymbol: "€"}); err != nil {
		t.Fatal(err)
	}
	if got := w.storedValue(t, "currency", "AccessKey"); got != "ak" {
		t.Fatalf("stored access key = %q, want the masked update to keep it", got)
	}
}

// A mask for a secret the store has no value of cannot be kept: the update
// is refused and nothing is written.
func TestMaskedSecretWithoutStoredValueIsRefused(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := adminContext()
	for name, update := range map[string]func() error{
		"verify":   func() error { return w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSecret: dto.SecretMask}) },
		"currency": func() error { return w.svc.UpdateCurrencyConfig(ctx, &dto.CurrencyConfig{AccessKey: dto.SecretMask}) },
		"node": func() error {
			return w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{Outbound: []dto.PlatformNodeOutboundSnapshot{{Name: "new", Password: dto.SecretMask}}})
		},
	} {
		if err := update(); xerr.CodeOf(err) != xerr.InvalidParams {
			t.Fatalf("%s: err = %v, want a parameter error", name, err)
		}
	}
	if calls := w.runtime.calls(); len(calls) != 0 {
		t.Fatalf("refused updates reloaded %v", calls)
	}
	if n := len(w.actions(t)); n != 0 {
		t.Fatalf("refused updates left %d audit rows", n)
	}
}

// The outbound credentials read back masked and are kept by name, or by
// position when the name is new, so a renamed outbound keeps its
// credentials and a reordered one does not take another's.
func TestOutboundSecretsAreMaskedAndKept(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := adminContext()
	stored := []dto.PlatformNodeOutboundSnapshot{
		{Name: "warp", Protocol: "wireguard", Address: "a", Port: 1, Password: "warp-pw", UUID: "warp-uuid"},
		{Name: "relay", Protocol: "vless", Address: "b", Port: 2, UUID: "relay-uuid", EncryptionPassword: "relay-enc"},
	}
	if err := w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{NodeSecret: "node-secret", Outbound: stored}); err != nil {
		t.Fatal(err)
	}
	got, err := w.svc.GetNodeConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeSecret != "node-secret" {
		t.Fatalf("node secret = %q, want it readable for node deployment", got.NodeSecret)
	}
	wantMasked := []dto.PlatformNodeOutboundSnapshot{
		{Name: "warp", Protocol: "wireguard", Address: "a", Port: 1, Password: dto.SecretMask, UUID: dto.SecretMask},
		{Name: "relay", Protocol: "vless", Address: "b", Port: 2, UUID: dto.SecretMask, EncryptionPassword: dto.SecretMask},
	}
	if !reflect.DeepEqual(got.Outbound, wantMasked) {
		t.Fatalf("outbounds = %+v, want %+v", got.Outbound, wantMasked)
	}

	storedOutbounds := func() []config.NodeOutbound {
		t.Helper()
		rows, err := w.system.GetNodeConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := system.ParseNodeConfig(rows)
		if err != nil {
			t.Fatal(err)
		}
		return parsed.Outbound
	}

	// Reordered, with a new outbound carrying its own credentials: the
	// masks resolve by name.
	reordered := []dto.PlatformNodeOutboundSnapshot{
		{Name: "relay", Protocol: "vless", Address: "b", Port: 2, UUID: dto.SecretMask, EncryptionPassword: dto.SecretMask},
		{Name: "warp", Protocol: "wireguard", Address: "a", Port: 1, Password: dto.SecretMask, UUID: dto.SecretMask},
		{Name: "fresh", Protocol: "trojan", Address: "c", Port: 3, Password: "fresh-pw"},
	}
	if err := w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{NodeSecret: "node-secret", Outbound: reordered}); err != nil {
		t.Fatal(err)
	}
	outbounds := storedOutbounds()
	if len(outbounds) != 3 {
		t.Fatalf("stored outbounds = %+v", outbounds)
	}
	if relay := outbounds[0]; relay.UUID != "relay-uuid" || relay.EncryptionPassword != "relay-enc" || relay.Password != "" {
		t.Fatalf("relay = %+v, want its credentials kept by name", relay)
	}
	if warp := outbounds[1]; warp.Password != "warp-pw" || warp.UUID != "warp-uuid" {
		t.Fatalf("warp = %+v, want its credentials kept by name", warp)
	}
	if fresh := outbounds[2]; fresh.Password != "fresh-pw" {
		t.Fatalf("fresh = %+v, want its own password", fresh)
	}

	// Renamed in place, with a replaced password on another: the mask of a
	// new name resolves by position.
	renamed := []dto.PlatformNodeOutboundSnapshot{
		{Name: "relay", Protocol: "vless", Address: "b", Port: 2, UUID: dto.SecretMask, EncryptionPassword: "relay-enc-2"},
		{Name: "warp-eu", Protocol: "wireguard", Address: "a", Port: 1, Password: dto.SecretMask, UUID: dto.SecretMask},
		{Name: "fresh", Protocol: "trojan", Address: "c", Port: 3, Password: dto.SecretMask},
	}
	if err := w.svc.UpdateNodeConfig(ctx, &dto.NodeConfig{NodeSecret: "node-secret", Outbound: renamed}); err != nil {
		t.Fatal(err)
	}
	outbounds = storedOutbounds()
	if relay := outbounds[0]; relay.UUID != "relay-uuid" || relay.EncryptionPassword != "relay-enc-2" {
		t.Fatalf("relay = %+v, want the encryption password replaced and the UUID kept", relay)
	}
	if warp := outbounds[1]; warp.Name != "warp-eu" || warp.Password != "warp-pw" || warp.UUID != "warp-uuid" {
		t.Fatalf("warp-eu = %+v, want the credentials of the outbound at its position", warp)
	}
	if fresh := outbounds[2]; fresh.Password != "fresh-pw" {
		t.Fatalf("fresh = %+v, want its password kept", fresh)
	}
}

// A malformed stored node document does not stop the update that fixes it.
func TestNodeConfigUpdateRepairsAMalformedDocument(t *testing.T) {
	w := newSettingsWorld(t)
	if err := w.system.UpdateValueByCategoryKey(context.Background(), "server", "Outbound", "{not json"); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.UpdateNodeConfig(adminContext(), &dto.NodeConfig{NodeSecret: "s", Outbound: []dto.PlatformNodeOutboundSnapshot{{Name: "warp", Password: "pw"}}}); err != nil {
		t.Fatalf("UpdateNodeConfig over a malformed document: %v", err)
	}
	if got, err := w.svc.GetNodeConfig(context.Background()); err != nil || len(got.Outbound) != 1 {
		t.Fatalf("node config after the repair = %+v (err %v)", got, err)
	}
}

// Every settings update leaves an audit row filed under the administrator,
// naming the category and the keys whose stored value changed — never a
// value, since several categories hold secrets — and no row when nothing
// changed.
func TestSettingsUpdatesAreAuditedByChangedKey(t *testing.T) {
	w := newSettingsWorld(t)
	ctx := adminContext()
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site", TurnstileSecret: "s3cret", EnableLoginVerify: true}); err != nil {
		t.Fatal(err)
	}
	// Only the site key changes; the secret is sent back masked.
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site-2", TurnstileSecret: dto.SecretMask, EnableLoginVerify: true}); err != nil {
		t.Fatal(err)
	}
	// Nothing changes.
	if err := w.svc.UpdateVerifyConfig(ctx, &dto.VerifyConfig{TurnstileSiteKey: "site-2", TurnstileSecret: dto.SecretMask, EnableLoginVerify: true}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.UpdateSiteConfig(ctx, &dto.SiteConfig{SiteName: "Panel"}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SetNodeMultiplier(ctx, &dto.SetNodeMultiplierRequest{Periods: []dto.TimePeriod{{StartTime: "00:00", EndTime: "06:00", Multiplier: 0.5}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.SettingTelegramBot(ctx); err != nil {
		t.Fatal(err)
	}

	actions := w.actions(t)
	want := []struct{ action, object, detail string }{
		{"settings.update", "verify", "keys: TurnstileSiteKey, TurnstileSecret, EnableLoginVerify"},
		{"settings.update", "verify", "keys: TurnstileSiteKey"},
		{"settings.update", "site", "keys: SiteName"},
		{"settings.update", "server", "keys: NodeMultiplierConfig"},
		{"settings.reload", "telegram", ""},
	}
	if len(actions) != len(want) {
		t.Fatalf("audited %d actions, want %d: %+v", len(actions), len(want), actions)
	}
	for i, tc := range want {
		got := actions[i]
		if got.Action != tc.action || got.Object != tc.object || got.Detail != tc.detail || got.ActorID != 9 || got.ClientIP != "203.0.113.9" ||
			got.Source != log.AdminActionSourceHTTP || got.Timestamp == 0 {
			t.Fatalf("action %d = %+v, want %+v by administrator 9", i, got, tc)
		}
		if strings.Contains(got.Detail, "s3cret") || strings.Contains(got.Detail, "site-2") {
			t.Fatalf("action %d records a value: %q", i, got.Detail)
		}
	}
}

// The audit row is written in the update's transaction: a rolled-back
// update leaves no trail of a change that did not happen.
func TestRolledBackSettingsUpdateLeavesNoAuditRow(t *testing.T) {
	w := newSettingsWorld(t)
	w.tx.failCommit = errors.New("commit failed")
	if err := w.svc.UpdateSiteConfig(adminContext(), &dto.SiteConfig{SiteName: "after"}); xerr.CodeOf(err) != xerr.DatabaseUpdateError {
		t.Fatalf("err = %v, want a database update error", err)
	}
	if n := len(w.actions(t)); n != 0 {
		t.Fatalf("a rolled-back update left %d audit rows", n)
	}
}
