package config

import (
	"strings"

	"gopkg.in/yaml.v3"

	"pr-size/internal/labels"
)

type labelOverride struct {
	Name    *string `yaml:"name"`
	Lines   *int    `yaml:"lines"`
	Color   *string `yaml:"color"`
	Comment *string `yaml:"comment"`
}

func LoadLabelSet(content string) (labels.Set, error) {
	set := labels.DefaultSet().Clone()
	if strings.TrimSpace(content) == "" {
		return set, nil
	}

	overrides := map[string]labelOverride{}
	if err := yaml.Unmarshal([]byte(content), &overrides); err != nil {
		return nil, err
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
