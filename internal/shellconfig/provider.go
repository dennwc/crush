package shellconfig

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"charm.land/catwalk/pkg/embedded"
	"github.com/charmbracelet/crush/internal/agent/hyper"
)

// handleProvider implements the `provider` builtin.
//
// Usage:
//
//	provider add <id> [--name NAME] [--type TYPE] [--api-key KEY]
//	    [--base-url URL] [--disable true|false] [--flat-rate true|false]
//	    [--discover-models true|false] [--system-prompt-prefix TEXT]
//	    [--extra-header KEY VALUE] [--extra-body JSON]
//	    [--provider-options JSON]
//	provider remove <id>   (alias: rm)
//	provider enable <id>|all
//	provider disable <id>|all
//
// "add" defines or updates a provider; repeated calls with the same <id>
// update the same entry. "remove" removes a provider and all its children.
// "enable" and "disable" toggle the provider's disable flag without touching
// its other settings; "all" applies to every provider known at that point in
// the script (the built-in catalog plus anything the script has defined so
// far). Disabled providers are hidden from the model selection list.
func handleProvider(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	b := configBuilderFromCtx(ctx)
	if b == nil {
		return nil
	}
	if len(args) < 2 {
		return usage(stderr, "usage: provider add <id> [flags] | provider remove <id> | provider enable|disable <id>|all")
	}

	switch args[1] {
	case "add":
		return providerAdd(b, args, stderr)
	case "remove", "rm":
		return providerRemove(b, args, stderr)
	case "enable", "disable":
		return providerToggle(b, args, stderr)
	default:
		return usage(stderr, fmt.Sprintf("provider: unknown subcommand %q (expected add, remove, enable, or disable)", args[1]))
	}
}

// providerAddFlags is the declarative flag surface for `provider add`.
var providerAddFlags = []flagSpec{
	{name: "--name", jsonKey: "name", kind: flagString, op: opSet},
	{name: "--type", jsonKey: "type", kind: flagString, op: opSet},
	{name: "--api-key", jsonKey: "api_key", kind: flagString, op: opSet},
	{name: "--base-url", jsonKey: "base_url", kind: flagString, op: opSet},
	{name: "--disable", jsonKey: "disable", kind: flagBool, op: opSet},
	{name: "--flat-rate", jsonKey: "flat_rate", kind: flagBool, op: opSet},
	{name: "--discover-models", jsonKey: "discover_models", kind: flagBool, op: opSet},
	{name: "--system-prompt-prefix", jsonKey: "system_prompt_prefix", kind: flagString, op: opSet},
	{name: "--extra-header", child: "extra_headers", kind: flagKeyValue, op: opSetChild},
	{name: "--extra-body", child: "extra_body", kind: flagJSONObject, op: opMergeChild},
	{name: "--provider-options", child: "provider_options", kind: flagJSONObject, op: opMergeChild},
}

func providerAdd(b *ConfigBuilder, args []string, stderr io.Writer) error {
	if len(args) < 3 {
		return usage(stderr, "usage: provider add <id> [--name NAME] [--type TYPE] [--api-key KEY] [--base-url URL] [--disable true|false] [--flat-rate true|false] [--discover-models true|false] [--system-prompt-prefix TEXT] [--extra-header KEY VALUE] [--extra-body JSON] [--provider-options JSON]")
	}
	id := args[2]
	slog.Info("Provider defined in shell config", "provider", id)
	p := childMap(b.section("providers"), id)

	if err := applyFlags(providerAddFlags, args, 3, p, "provider add", stderr); err != nil {
		return err
	}

	slog.Debug("Provider recorded", "provider", id)
	return nil
}

func providerRemove(b *ConfigBuilder, args []string, stderr io.Writer) error {
	if len(args) < 3 {
		return usage(stderr, "usage: provider remove <id>")
	}
	id := args[2]
	delete(b.section("providers"), id)
	slog.Info("Provider removed in shell config", "provider", id)
	return nil
}

func providerToggle(b *ConfigBuilder, args []string, stderr io.Writer) error {
	verb := args[1]
	if len(args) < 3 {
		return usage(stderr, fmt.Sprintf("usage: provider %s <id>|all", verb))
	}
	arg := args[2]

	ids := []string{arg}
	if arg == "all" {
		ids = allProviderIDs(b)
	}

	// Only set the disable flag so a toggle on a known provider keeps its
	// catalog entry (models, credentials) instead of replacing it.
	disabled := verb == "disable"
	providers := b.section("providers")
	for _, id := range ids {
		p := childMap(providers, id)
		p["disable"] = disabled
	}
	slog.Info("Provider enablement changed in shell config", "provider", arg, "disabled", disabled)
	return nil
}

// allProviderIDs lists every provider known at this point in the script: the
// built-in catalog shipped with this release (including Hyper, which is not
// part of the embedded catalog), plus anything the script has defined so far.
func allProviderIDs(b *ConfigBuilder) []string {
	seen := make(map[string]bool)
	var ids []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	add(hyper.Name)
	for _, p := range embedded.GetAll() {
		add(string(p.ID))
	}
	for id := range b.section("providers") {
		add(id)
	}
	slices.Sort(ids)
	return ids
}
