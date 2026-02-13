package paths

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRelPath(t *testing.T) {
	assert.NoError(t, ValidateRelPath("foo/bar.go"))
	assert.NoError(t, ValidateRelPath("a.txt"))
	assert.NoError(t, ValidateRelPath("deep/nested/path/file.go"))
	assert.NoError(t, ValidateRelPath("file with spaces.go"))
	assert.NoError(t, ValidateRelPath("日本語.txt"))
	assert.NoError(t, ValidateRelPath("a/b/c/d/e/f/g/h/i/j.txt"))

	assert.Error(t, ValidateRelPath(""))
	assert.Error(t, ValidateRelPath("/absolute/path"))
	assert.Error(t, ValidateRelPath("../escape"))
	assert.Error(t, ValidateRelPath("foo/../../etc/passwd"))
	assert.Error(t, ValidateRelPath("foo\x00bar"))
	assert.Error(t, ValidateRelPath("."))
	assert.Error(t, ValidateRelPath("./"))
}

func TestValidateRelPathTraversalVariants(t *testing.T) {
	cases := []string{
		"../",
		"foo/../../../etc/shadow",
		"a/b/c/../../../../tmp/x",
		"..",
	}
	for _, c := range cases {
		assert.Error(t, ValidateRelPath(c), "should reject: %q", c)
	}
}

func TestCleanRelPath(t *testing.T) {
	assert.Equal(t, "foo/bar", CleanRelPath("./foo/bar"))
	assert.Equal(t, "foo/bar", CleanRelPath("foo//bar"))
	assert.Equal(t, "foo/bar", CleanRelPath("foo/./bar"))
	assert.Equal(t, "foo", CleanRelPath("foo/bar/.."))
	assert.Equal(t, "a/b", CleanRelPath("./a/./b"))
}

func TestIsWithinDir(t *testing.T) {
	assert.True(t, IsWithinDir(
		"/home/user/project",
		"/home/user/project/foo",
	))
	assert.True(t, IsWithinDir(
		"/home/user/project/",
		"/home/user/project/foo",
	))
	assert.True(t, IsWithinDir(
		"/home/user/project",
		"/home/user/project",
	))

	assert.False(t, IsWithinDir(
		"/home/user/project",
		"/home/user/other",
	))
	assert.False(t, IsWithinDir(
		"/home/user/project",
		"/etc/passwd",
	))
	assert.False(t, IsWithinDir(
		"/home/user/project",
		"/home/user/projectX/foo",
	))
	assert.False(t, IsWithinDir(
		"/tmp/a",
		"/tmp/ab/c",
	))
}
