package generated

import (
	"bufio"
	"regexp"
	"strings"

	"path"

	"github.com/bmatcuk/doublestar/v4"
)

var generatedPattern = regexp.MustCompile(`(^|\s)linguist-generated(?:=true)?(\s|$)`)

func ParseGitattributes(content string) []string {
	patterns := []string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !generatedPattern.MatchString(line) || strings.Contains(line, "linguist-generated=false") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		patterns = append(patterns, fields[0])
	}
	return patterns
}

func Match(filePath string, patterns []string) bool {
	cleanPath := strings.TrimPrefix(path.Clean(filePath), "./")
	base := path.Base(cleanPath)
	for _, pattern := range patterns {
		if matchPattern(cleanPath, base, pattern) {
			return true
		}
	}
	return false
}

func matchPattern(cleanPath, base, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	rootRelative := strings.TrimPrefix(pattern, "/")

	if strings.Contains(rootRelative, "/") || strings.HasPrefix(pattern, "/") {
		if ok, _ := doublestar.Match(rootRelative, cleanPath); ok {
			return true
		}
		if strings.HasSuffix(rootRelative, "/") && strings.HasPrefix(cleanPath, rootRelative) {
			return true
		}
		return false
	}

	if ok, _ := doublestar.Match(rootRelative, base); ok {
		return true
	}
	return false
}
