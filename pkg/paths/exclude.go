package paths

import (
	"path/filepath"
	"strings"
)

type ExcludeMatcher struct {
	patterns []string
}

func NewExcludeMatcher(patterns []string) *ExcludeMatcher {
	return &ExcludeMatcher{patterns: patterns}
}

func (m *ExcludeMatcher) Match(relPath string) bool {
	for _, pat := range m.patterns {
		if matchPattern(pat, relPath) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, relPath string) bool {
	pattern = strings.TrimSuffix(pattern, "/")
	if strings.Contains(pattern, "/") {
		return matchPathPattern(pattern, relPath)
	}
	parts := strings.Split(relPath, "/")
	for _, part := range parts {
		if matched, _ := filepath.Match(pattern, part); matched {
			return true
		}
	}
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, relPath)
	}
	return false
}

func matchPathPattern(pattern, relPath string) bool {
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, relPath)
	}
	matched, _ := filepath.Match(pattern, relPath)
	return matched
}

func matchDoublestar(pattern, relPath string) bool {
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return false
	}
	prefix := strings.TrimSuffix(parts[0], "/")
	suffix := strings.TrimPrefix(parts[1], "/")

	if prefix == "" && suffix == "" {
		return true
	}
	if prefix == "" {
		return matchSuffix(suffix, relPath)
	}
	if suffix == "" {
		return strings.HasPrefix(relPath, prefix+"/") ||
			relPath == prefix
	}
	if !strings.HasPrefix(relPath, prefix+"/") {
		return false
	}
	return matchSuffix(
		suffix,
		strings.TrimPrefix(relPath, prefix+"/"),
	)
}

func matchSuffix(suffix, relPath string) bool {
	parts := strings.Split(relPath, "/")
	for i := range parts {
		tail := strings.Join(parts[i:], "/")
		if matched, _ := filepath.Match(suffix, tail); matched {
			return true
		}
	}
	return false
}
