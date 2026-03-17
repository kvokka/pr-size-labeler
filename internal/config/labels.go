package config

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"pr-size-labeler/internal/labels"
)

type LabelsConfig struct {
	Backfill BackfillConfig
	Labels   labels.Set
}

type BackfillConfig struct {
	Enabled  bool
	Lookback time.Duration
}

type labelOverride struct {
	Name    *string `yaml:"name"`
	Lines   *int    `yaml:"lines"`
	Symbols *int    `yaml:"symbols"`
	Color   *string `yaml:"color"`
	Comment *string `yaml:"comment"`
}

type labelsFileSchema struct {
	Backfill backfillSchema           `yaml:"backfill"`
	Labels   map[string]labelOverride `yaml:"labels"`
}

type backfillSchema struct {
	Enabled  *bool  `yaml:"enabled"`
	Lookback string `yaml:"lookback"`
}

const defaultBackfillLookback = 30 * 24 * time.Hour

func LoadLabelsConfig(content string) (LabelsConfig, error) {
	if strings.TrimSpace(content) == "" {
		return LabelsConfig{
			Backfill: BackfillConfig{Lookback: defaultBackfillLookback},
			Labels:   labels.DefaultSet().Clone(),
		}, nil
	}

	var parsed labelsFileSchema
	decoder := yaml.NewDecoder(bytes.NewBufferString(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&parsed); err != nil {
		return LabelsConfig{}, err
	}

	resolvedBackfill, err := resolveBackfillConfig(parsed.Backfill)
	if err != nil {
		return LabelsConfig{}, err
	}
	resolvedLabels, err := resolveLabelSet(parsed.Labels)
	if err != nil {
		return LabelsConfig{}, err
	}

	return LabelsConfig{
		Backfill: resolvedBackfill,
		Labels:   resolvedLabels,
	}, nil
}

func resolveBackfillConfig(raw backfillSchema) (BackfillConfig, error) {
	cfg := BackfillConfig{Lookback: defaultBackfillLookback}
	if raw.Enabled != nil {
		cfg.Enabled = *raw.Enabled
	}
	lookback := strings.TrimSpace(raw.Lookback)
	if lookback == "" {
		return cfg, nil
	}

	duration, err := time.ParseDuration(lookback)
	if err != nil {
		return BackfillConfig{}, fmt.Errorf("parse backfill.lookback: %w", err)
	}
	if duration <= 0 {
		return BackfillConfig{}, errors.New("backfill.lookback must be greater than 0")
	}
	cfg.Lookback = duration
	return cfg, nil
}

func resolveLabelSet(overrides map[string]labelOverride) (labels.Set, error) {
	set := labels.DefaultSet().Clone()
	for key := range overrides {
		if _, ok := set[key]; !ok {
			return nil, fmt.Errorf("labels.%s is not a supported size key", key)
		}
	}

	for key, def := range set {
		override, ok := overrides[key]
		if !ok {
			continue
		}
		merged := def
		if override.Name != nil {
			merged.Name = *override.Name
		}
		if override.Lines != nil {
			merged.Lines = *override.Lines
		}
		if override.Symbols != nil {
			symbols := *override.Symbols
			merged.Symbols = &symbols
		}
		if override.Color != nil {
			merged.Color = *override.Color
		}
		if override.Comment != nil {
			merged.Comment = *override.Comment
		}
		set[key] = merged
	}

	return set, nil
}
