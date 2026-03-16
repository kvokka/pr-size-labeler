package config

import "testing"

func TestLoadLabelSetMergesDefaults(t *testing.T) {
	content := `
S:
  name: custom/small
  color: 123456
  comment: keep it tiny
`

	set, err := LoadLabelSet(content)
	if err != nil {
		t.Fatalf("LoadLabelSet returned error: %v", err)
	}

	if got := set["S"].Name; got != "custom/small" {
		t.Fatalf("S name = %q, want custom/small", got)
	}
	if got := set["S"].Color; got != "123456" {
		t.Fatalf("S color = %q, want 123456", got)
	}
	if got := set["S"].Comment; got != "keep it tiny" {
		t.Fatalf("S comment = %q, want keep it tiny", got)
	}
	if got := set["S"].Lines; got != 10 {
		t.Fatalf("S lines = %d, want 10", got)
	}
	if got := set["XL"].Name; got != "size/XL" {
		t.Fatalf("XL name = %q, want size/XL", got)
	}
}
