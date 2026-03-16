package generated

import "testing"

func TestExcludeLinguistGeneratedFiles(t *testing.T) {
	content := `
# comment
*.snap linguist-generated=true
vendor/** linguist-generated=true
docs/*.md linguist-generated=false
generated/*.go linguist-generated
`

	patterns := ParseGitattributes(content)
	if len(patterns) != 3 {
		t.Fatalf("expected 3 patterns, got %d", len(patterns))
	}

	tests := []struct {
		path string
		want bool
	}{
		{"ui/button.snap", true},
		{"vendor/github.com/pkg/errors/errors.go", true},
		{"generated/client.go", true},
		{"docs/readme.md", false},
		{"cmd/pr-size/main.go", false},
	}

	for _, tt := range tests {
		if got := Match(tt.path, patterns); got != tt.want {
			t.Fatalf("Match(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestRootAnchoredPatternsStayRootRelative(t *testing.T) {
	patterns := ParseGitattributes(`/generated/* linguist-generated=true`)

	if !Match("generated/client.go", patterns) {
		t.Fatal("expected rooted pattern to match root path")
	}
	if Match("pkg/generated/client.go", patterns) {
		t.Fatal("expected rooted pattern to not match nested path")
	}
}
