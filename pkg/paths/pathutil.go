package paths

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

func ValidateRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("path contains null byte")
	}
	if path.IsAbs(p) {
		return fmt.Errorf("absolute path not allowed: %s", p)
	}
	cleaned := path.Clean(p)
	if cleaned == "." {
		return fmt.Errorf("path resolves to current directory")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf(
			"path escapes base directory: %s", p,
		)
	}
	return nil
}

func CleanRelPath(p string) string {
	p = path.Clean(p)
	p = strings.TrimPrefix(p, "./")
	return p
}

func IsWithinDir(dir, full string) bool {
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		return false
	}
	return rel != ".." &&
		!strings.HasPrefix(rel, "../") &&
		!filepath.IsAbs(rel)
}
