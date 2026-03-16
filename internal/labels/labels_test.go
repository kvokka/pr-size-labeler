package labels

import "testing"

func TestSizeThresholds(t *testing.T) {
	set := DefaultSet()
	tests := []struct {
		lines int
		want  string
	}{
		{0, "size/XS"},
		{9, "size/XS"},
		{10, "size/S"},
		{29, "size/S"},
		{30, "size/M"},
		{99, "size/M"},
		{100, "size/L"},
		{499, "size/L"},
		{500, "size/XL"},
		{999, "size/XL"},
		{1000, "size/XXL"},
	}

	for _, tt := range tests {
		if got := set.Select(tt.lines).Name; got != tt.want {
			t.Fatalf("Select(%d) = %q, want %q", tt.lines, got, tt.want)
		}
	}
}
