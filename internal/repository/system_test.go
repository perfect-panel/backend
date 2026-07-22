package repository

import (
	"slices"
	"testing"

	"github.com/perfect-panel/server/internal/config"
)

func TestSystemCategoryCacheKeys(t *testing.T) {
	keys := systemCategoryCacheKeys("site")
	for _, want := range []string{config.SiteConfigKey, config.GlobalConfigKey} {
		if !slices.Contains(keys, want) {
			t.Fatalf("site cache keys missing %q: %v", want, keys)
		}
	}
	if keys := systemCategoryCacheKeys("log"); len(keys) != 0 {
		t.Fatalf("uncached category should have no cache keys: %v", keys)
	}
}
