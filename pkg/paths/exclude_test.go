package paths

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExcludeBareName(t *testing.T) {
	m := NewExcludeMatcher([]string{"vendor"})
	assert.True(t, m.Match("vendor"))
	assert.True(t, m.Match("src/vendor"))
	assert.True(t, m.Match("a/b/vendor"))
	assert.False(t, m.Match("vendor.go"))
	assert.True(t, m.Match("vendor/pkg/mod"))
}

func TestExcludeTrailingSlash(t *testing.T) {
	m := NewExcludeMatcher([]string{"logs/"})
	assert.True(t, m.Match("logs"))
	assert.True(t, m.Match("src/logs"))
	assert.True(t, m.Match("logs/app.log"))
}

func TestExcludeWildcardExtension(t *testing.T) {
	m := NewExcludeMatcher([]string{"*.o"})
	assert.True(t, m.Match("main.o"))
	assert.True(t, m.Match("src/util.o"))
	assert.True(t, m.Match("deep/nested/thing.o"))
	assert.False(t, m.Match("main.go"))
	assert.False(t, m.Match("foo.obj"))
}

func TestExcludeQuestionMark(t *testing.T) {
	m := NewExcludeMatcher([]string{"?.tmp"})
	assert.True(t, m.Match("a.tmp"))
	assert.True(t, m.Match("src/x.tmp"))
	assert.False(t, m.Match("ab.tmp"))
	assert.False(t, m.Match("long.tmp"))
}

func TestExcludeDoublestarPrefix(t *testing.T) {
	m := NewExcludeMatcher([]string{"**/*.test.js"})
	assert.True(t, m.Match("foo.test.js"))
	assert.True(t, m.Match("src/foo.test.js"))
	assert.True(t, m.Match("a/b/c/d.test.js"))
	assert.False(t, m.Match("foo.js"))
	assert.False(t, m.Match("src/foo.spec.js"))
}

func TestExcludeDoublestarMiddle(t *testing.T) {
	m := NewExcludeMatcher([]string{"src/**/*.pb.go"})
	assert.True(t, m.Match("src/api/v1/types.pb.go"))
	assert.True(t, m.Match("src/schema.pb.go"))
	assert.False(t, m.Match("pkg/types.pb.go"))
	assert.False(t, m.Match("src/api/v1/types.go"))
}

func TestExcludeDoublestarAlone(t *testing.T) {
	m := NewExcludeMatcher([]string{"**"})
	assert.True(t, m.Match("anything"))
	assert.True(t, m.Match("a/b/c"))
}

func TestExcludeDoublestarSuffix(t *testing.T) {
	m := NewExcludeMatcher([]string{"build/**"})
	assert.True(t, m.Match("build/output.js"))
	assert.True(t, m.Match("build/dist/bundle.js"))
	assert.False(t, m.Match("src/build.go"))
}

func TestExcludePathPattern(t *testing.T) {
	m := NewExcludeMatcher([]string{"doc/*.html"})
	assert.True(t, m.Match("doc/index.html"))
	assert.False(t, m.Match("doc/sub/page.html"))
	assert.False(t, m.Match("other/index.html"))
}

func TestExcludeMultiplePatterns(t *testing.T) {
	m := NewExcludeMatcher([]string{
		"*.pyc",
		"__pycache__",
		".git",
		"*.swp",
		"node_modules",
		".DS_Store",
		"*.o",
		"build/",
	})

	assert.True(t, m.Match("foo.pyc"))
	assert.True(t, m.Match("src/__pycache__"))
	assert.True(t, m.Match(".git"))
	assert.True(t, m.Match("src/main.go.swp"))
	assert.True(t, m.Match("node_modules"))
	assert.True(t, m.Match(".DS_Store"))
	assert.True(t, m.Match("src/lib.o"))
	assert.True(t, m.Match("build"))

	assert.False(t, m.Match("src/main.go"))
	assert.False(t, m.Match("README.md"))
	assert.False(t, m.Match("Makefile"))
}

func TestExcludeEmptyPatterns(t *testing.T) {
	m := NewExcludeMatcher(nil)
	assert.False(t, m.Match("anything"))
	assert.False(t, m.Match("a/b/c.go"))
}

func TestExcludeDotfiles(t *testing.T) {
	m := NewExcludeMatcher([]string{".env", ".env.*"})
	assert.True(t, m.Match(".env"))
	assert.True(t, m.Match(".env.local"))
	assert.True(t, m.Match("deploy/.env"))
	assert.True(t, m.Match("deploy/.env.production"))
	assert.False(t, m.Match("env"))
	assert.False(t, m.Match("dotenv.go"))
}
