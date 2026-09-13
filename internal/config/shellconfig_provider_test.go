package config_test

import (
	"slices"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestShellConfigProviderAddAndModel(t *testing.T) {
	store := loadCrushSh(t, `provider add myllm \
  --type openai-compat \
  --base-url "http://localhost:1234/v1" \
  --api-key "sk-test" \
  --discover-models false \
  --extra-body '{"service_tier":"flex"}' \
  --provider-options '{"region":"local"}'
provider add myllm \
  --extra-body '{"stream":true}' \
  --provider-options '{"mode":"test"}'
model add myllm/foo-1 --name "Foo 1" --context-window 8000 \
  --price-input 1.25 --price-output 5 \
  --price-cache-create 2 --price-cache-hit 0.25
model large myllm/foo-1 \
  --top-p 0.9 --top-k 40 \
  --frequency-penalty 0.2 --presence-penalty 0.1 \
  --provider-options '{"routing":{"tier":"fast"}}'
model large myllm/foo-1 --provider-options '{"timeout":30}'`)

	cfg := store.Config()

	p, ok := cfg.Providers.Get("myllm")
	require.True(t, ok, "myllm provider should be configured")
	require.Equal(t, "sk-test", p.APIKey)
	require.Equal(t, "http://localhost:1234/v1", p.BaseURL)
	require.NotNil(t, p.AutoDiscoverModels)
	require.False(t, *p.AutoDiscoverModels)
	require.Equal(t, "flex", p.ExtraBody["service_tier"])
	require.Equal(t, true, p.ExtraBody["stream"])
	require.Equal(t, "local", p.ProviderOptions["region"])
	require.Equal(t, "test", p.ProviderOptions["mode"])
	require.True(
		t,
		slices.ContainsFunc(p.Models, func(m catwalk.Model) bool { return m.ID == "foo-1" }),
		"custom model foo-1 should be in the provider catalog",
	)
	model := p.Models[0]
	require.Equal(t, 1.25, model.CostPer1MIn)
	require.Equal(t, 5.0, model.CostPer1MOut)
	require.Equal(t, 2.0, model.CostPer1MInCached)
	require.Equal(t, 0.25, model.CostPer1MOutCached)

	large := cfg.Models[config.SelectedModelTypeLarge]
	require.Equal(t, "myllm", large.Provider)
	require.Equal(t, "foo-1", large.Model)
	require.NotNil(t, large.TopP)
	require.Equal(t, 0.9, *large.TopP)
	require.NotNil(t, large.TopK)
	require.Equal(t, int64(40), *large.TopK)
	require.NotNil(t, large.FrequencyPenalty)
	require.Equal(t, 0.2, *large.FrequencyPenalty)
	require.NotNil(t, large.PresencePenalty)
	require.Equal(t, 0.1, *large.PresencePenalty)
	require.Equal(t, map[string]any{
		"routing": map[string]any{"tier": "fast"},
		"timeout": float64(30),
	}, large.ProviderOptions)
}

func TestShellConfigProviderRemove(t *testing.T) {
	// Both providers get a model so they survive provider configuration
	// (model-less providers are dropped); the only difference is the remove.
	store := loadCrushSh(t, `provider add keepme --type openai-compat --base-url "http://localhost:1/v1" --api-key k
model add keepme/m1 --name M1
provider add dropme --type openai-compat --base-url "http://localhost:2/v1" --api-key k
model add dropme/m2 --name M2
provider remove dropme`)

	_, keep := store.Config().Providers.Get("keepme")
	_, drop := store.Config().Providers.Get("dropme")
	require.True(t, keep, "keepme should remain")
	require.False(t, drop, "dropme should be gone after remove")
}

func TestShellConfigProviderDisable(t *testing.T) {
	store := loadCrushSh(t, `provider add myllm --type openai-compat --base-url "http://localhost:1234/v1" --api-key sk-test --discover-models false
model add myllm/m1 --name M1
provider disable anthropic`)

	cfg := store.Config()

	_, ok := cfg.Providers.Get("myllm")
	require.True(t, ok, "myllm provider should be configured")

	require.False(t, containsEnabledProvider(cfg, "anthropic"), "anthropic should be disabled")
	require.True(t, containsEnabledProvider(cfg, "myllm"), "myllm should stay enabled")
}

// The canonical disable-all-then-enable-one flow: everything in the built-in
// catalog is disabled, then Hyper is enabled again. Only Hyper survives in
// the enabled set, so it is the only provider in the model selection list.
func TestShellConfigProviderDisableAllThenEnableHyper(t *testing.T) {
	t.Setenv("HYPER_API_KEY", "sk-hyper")
	store := loadCrushSh(t, `provider disable all
provider enable hyper`)

	cfg := store.Config()

	require.True(t, containsEnabledProvider(cfg, "hyper"), "hyper should be the only enabled provider")
	require.False(t, containsEnabledProvider(cfg, "anthropic"), "anthropic should be disabled")
	require.False(t, containsEnabledProvider(cfg, "openai"), "openai should be disabled")

	hyper, ok := cfg.Providers.Get("hyper")
	require.True(t, ok, "hyper provider should be configured")
	require.False(t, hyper.Disable, "hyper should not carry the disable flag")
}

// The last toggle in the script wins: disabling all after enabling a provider
// disables it too, and a later enable brings a provider back.
func TestShellConfigProviderToggleOrderWins(t *testing.T) {
	store := loadCrushSh(t, `provider add myllm --type openai-compat --base-url "http://localhost:1234/v1" --api-key sk-test --discover-models false
model add myllm/m1 --name M1
provider enable anthropic
provider disable all
provider enable myllm`)

	cfg := store.Config()
	require.False(t, containsEnabledProvider(cfg, "anthropic"), "anthropic should be disabled by the later disable all")
	require.True(t, containsEnabledProvider(cfg, "myllm"), "myllm should be re-enabled after disable all")
}

func TestShellConfigProviderToggleRequiresArgument(t *testing.T) {
	for _, verb := range []string{"enable", "disable"} {
		_, err := loadCrushShErr(t, "provider "+verb)
		require.Error(t, err)
		require.Contains(t, err.Error(), "usage: provider "+verb)
	}
}

// containsEnabledProvider reports whether id is configured and not disabled.
// Disabled known providers stay in the provider map with the flag set, while
// disabled custom ones are dropped entirely; either way they must not show up
// as enabled, which is what the model selection list is built from.
func containsEnabledProvider(cfg *config.Config, id string) bool {
	for _, p := range cfg.EnabledProviders() {
		if p.ID == id {
			return true
		}
	}
	return false
}
