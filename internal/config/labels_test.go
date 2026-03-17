package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadLabelsConfigMergesDefaults(t *testing.T) {
	content := `
labels:
  S:
    name: custom/small
    color: 123456
    comment: keep it tiny
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}

	if cfg.Backfill.Enabled {
		t.Fatal("expected backfill to be disabled")
	}
	if cfg.Backfill.Lookback != defaultBackfillLookback {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, defaultBackfillLookback)
	}
	if got := cfg.Labels["S"].Name; got != "custom/small" {
		t.Fatalf("S name = %q, want custom/small", got)
	}
	if got := cfg.Labels["S"].Color; got != "123456" {
		t.Fatalf("S color = %q, want 123456", got)
	}
	if got := cfg.Labels["S"].Comment; got != "keep it tiny" {
		t.Fatalf("S comment = %q, want keep it tiny", got)
	}
	if got := cfg.Labels["S"].Lines; got != 10 {
		t.Fatalf("S lines = %d, want 10", got)
	}
	if cfg.Labels["S"].Symbols != nil {
		t.Fatal("S symbols should be nil when not explicitly configured")
	}
	if got := cfg.Labels["XL"].Name; got != "size/XL" {
		t.Fatalf("XL name = %q, want size/XL", got)
	}
}

func TestLoadLabelsConfigSupportsExplicitSymbolsAndBackfillLookback(t *testing.T) {
	content := `
backfill:
  enabled: true
  lookback: 168h
labels:
  L:
    symbols: 250
  XS:
    symbols: 0
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}

	if !cfg.Backfill.Enabled {
		t.Fatal("expected backfill to be enabled")
	}
	if cfg.Backfill.Lookback != 168*time.Hour {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, 168*time.Hour)
	}
	if cfg.Labels["L"].Symbols == nil || *cfg.Labels["L"].Symbols != 250 {
		t.Fatalf("L symbols = %v, want 250", cfg.Labels["L"].Symbols)
	}
	if cfg.Labels["XS"].Symbols == nil || *cfg.Labels["XS"].Symbols != 0 {
		t.Fatalf("XS symbols = %v, want 0", cfg.Labels["XS"].Symbols)
	}
}

func TestLoadLabelsConfigLeavesSymbolsUnsetWhenOnlyLinesAreOverridden(t *testing.T) {
	content := `
labels:
  L:
    lines: 7
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}

	if got := cfg.Labels["L"].Lines; got != 7 {
		t.Fatalf("L lines = %d, want 7", got)
	}
	if cfg.Labels["L"].Symbols != nil {
		t.Fatal("L symbols should remain nil when only lines are overridden")
	}
	if got := cfg.Labels["L"].ResolvedSymbols(); got != 700 {
		t.Fatalf("L resolved symbols = %d, want 700", got)
	}
}

func TestLoadLabelsConfigRejectsLegacyFlatSchema(t *testing.T) {
	content := `
S:
  name: custom/small
`

	_, err := LoadLabelsConfig(content)
	if err == nil {
		t.Fatal("expected LoadLabelsConfig to reject the legacy flat schema")
	}
}

func TestLoadLabelsConfigUsesDefaultsWhenLabelsSectionMissing(t *testing.T) {
	content := `
backfill:
  enabled: false
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}
	if got := cfg.Labels["XS"].Name; got != "size/XS" {
		t.Fatalf("XS name = %q, want size/XS", got)
	}
	if got := cfg.Labels["XXL"].Lines; got != 1000 {
		t.Fatalf("XXL lines = %d, want 1000", got)
	}
}

func TestLoadLabelsConfigRejectsInvalidDuration(t *testing.T) {
	content := `
backfill:
  enabled: true
  lookback: 1y
labels: {}
`

	_, err := LoadLabelsConfig(content)
	if err == nil {
		t.Fatal("expected LoadLabelsConfig to reject invalid lookback")
	}
	if !strings.Contains(err.Error(), "backfill.lookback") {
		t.Fatalf("error = %q, want message about backfill.lookback", err.Error())
	}
}

func TestLoadLabelsConfigRejectsNonPositiveBackfillLookback(t *testing.T) {
	content := `
backfill:
  enabled: true
  lookback: 0s
labels: {}
`

	_, err := LoadLabelsConfig(content)
	if err == nil {
		t.Fatal("expected LoadLabelsConfig to reject non-positive lookback")
	}
	if err.Error() != "backfill.lookback must be greater than 0" {
		t.Fatalf("error = %q, want %q", err.Error(), "backfill.lookback must be greater than 0")
	}
}

func TestLoadLabelsConfigUsesDefaultLookbackWhenBackfillEnabled(t *testing.T) {
	content := `
backfill:
  enabled: true
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}
	if !cfg.Backfill.Enabled {
		t.Fatal("expected backfill to be enabled")
	}
	if cfg.Backfill.Lookback != defaultBackfillLookback {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, defaultBackfillLookback)
	}
}

func TestLoadLabelsConfigUsesDefaultsWhenBackfillSectionMissing(t *testing.T) {
	content := `
labels: {}
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}
	if cfg.Backfill.Enabled {
		t.Fatal("expected backfill to default to disabled")
	}
	if cfg.Backfill.Lookback != defaultBackfillLookback {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, defaultBackfillLookback)
	}
}

func TestLoadLabelsConfigUsesDefaultsForEmptyContent(t *testing.T) {
	cfg, err := LoadLabelsConfig("")
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}
	if cfg.Backfill.Enabled {
		t.Fatal("expected backfill to default to disabled")
	}
	if cfg.Backfill.Lookback != defaultBackfillLookback {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, defaultBackfillLookback)
	}
	if got := cfg.Labels["M"].Name; got != "size/M" {
		t.Fatalf("M name = %q, want size/M", got)
	}
}

func TestLoadLabelsConfigUsesBackfillDefaultsWhenFieldsMissing(t *testing.T) {
	content := `
backfill:
  lookback: 168h
`

	cfg, err := LoadLabelsConfig(content)
	if err != nil {
		t.Fatalf("LoadLabelsConfig returned error: %v", err)
	}
	if cfg.Backfill.Enabled {
		t.Fatal("expected backfill.enabled to default to false")
	}
	if cfg.Backfill.Lookback != 168*time.Hour {
		t.Fatalf("backfill lookback = %s, want %s", cfg.Backfill.Lookback, 168*time.Hour)
	}
}

func TestLoadLabelsConfigRejectsUnknownTopLevelKeys(t *testing.T) {
	content := `
backfill:
  enabled: false
labels: {}
extra: true
`

	_, err := LoadLabelsConfig(content)
	if err == nil {
		t.Fatal("expected LoadLabelsConfig to reject unknown top-level keys")
	}
}

func TestLoadLabelsConfigRejectsUnknownLabelKeys(t *testing.T) {
	content := `
backfill:
  enabled: false
labels:
  tiny:
    name: size/tiny
`

	_, err := LoadLabelsConfig(content)
	if err == nil {
		t.Fatal("expected LoadLabelsConfig to reject unknown label keys")
	}
	if err.Error() != `labels.tiny is not a supported size key` {
		t.Fatalf("error = %q, want %q", err.Error(), `labels.tiny is not a supported size key`)
	}
}
