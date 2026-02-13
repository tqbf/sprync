package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tqbf/sprync/pkg/pack"
)

func buildSpryncd(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "spryncd")
	cmd := exec.Command(
		"go", "build", "-o", bin, "./cmd/spryncd",
	)
	cmd.Dir = findModRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "build spryncd: %s", out)
	return bin
}

func findModRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(
			filepath.Join(dir, "go.mod"),
		); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod")
		}
		dir = parent
	}
}

func makeTree(
	t *testing.T,
	dir string,
	files map[string]string,
) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t,
			os.MkdirAll(filepath.Dir(full), 0755),
		)
		require.NoError(t,
			os.WriteFile(full, []byte(content), 0644),
		)
	}
}

func TestSessionReady(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	assert.Equal(t, "0.1.0", s.Version)
	assert.NotZero(t, s.PID)
}

func TestManifestExistingDir(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"main.go":     "package main",
		"src/util.go": "package src",
		"README.md":   "hello",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 3)

	pathSet := map[string]bool{}
	for _, e := range entries {
		pathSet[e.Path] = true
		assert.NotEmpty(t, e.Hash)
		assert.NotZero(t, e.Mode)
		assert.NotZero(t, e.Size)
	}
	assert.True(t, pathSet["main.go"])
	assert.True(t, pathSet["src/util.go"])
	assert.True(t, pathSet["README.md"])
}

func TestManifestWithExcludes(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"main.go":          "package main",
		"node_modules/a.js": "module",
		"test.pyc":         "bytecode",
	})

	entries, exists, _, err := s.Manifest(
		dir, []string{"node_modules", "*.pyc"},
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, "main.go", entries[0].Path)
}

func TestManifestNonexistentDir(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	entries, exists, _, err := s.Manifest(
		"/nonexistent/path/xyz", nil,
	)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)
}

func TestManifestEmptyDir(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 0)
}

func TestManifestHashConsistency(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.txt": "hello world",
		"b.txt": "hello world",
		"c.txt": "different content",
	})

	entries, _, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	byPath := map[string]string{}
	for _, e := range entries {
		byPath[e.Path] = e.Hash
	}
	assert.Equal(t, byPath["a.txt"], byPath["b.txt"])
	assert.NotEqual(t, byPath["a.txt"], byPath["c.txt"])
}

func TestPackStub(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.go": "package a",
		"b.go": "package b",
	})

	result, err := s.Pack(
		dir,
		[]string{"a.go", "b.go"},
		"/tmp/sprync-test.tar.gz",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/sprync-test.tar.gz", result.Dest)
	assert.Equal(t, 2, result.Count)
}

func TestExtractReal(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	srcDir := t.TempDir()
	makeTree(t, srcDir, map[string]string{
		"hello.go": "package hello",
	})

	tarPath := "/tmp/sprync-extract-test.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(
		srcDir, []string{"hello.go"}, f, true,
	)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	destDir := t.TempDir()
	result, err := s.Extract(destDir, tarPath, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)

	got, err := os.ReadFile(
		filepath.Join(destDir, "hello.go"),
	)
	require.NoError(t, err)
	assert.Equal(t, "package hello", string(got))
}

func TestDeleteStub(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"old.go": "delete me",
	})

	result, err := s.Delete(dir, []string{"old.go"})
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)
}

func TestMultipleCommands(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.go":  "package a",
		"b.go":  "package b",
		"c.txt": "text",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 3)

	packResult, err := s.Pack(
		dir,
		[]string{"a.go"},
		"/tmp/sprync-multi.tar.gz",
		true,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, packResult.Count)

	delResult, err := s.Delete(dir, []string{"c.txt"})
	require.NoError(t, err)
	assert.Equal(t, 1, delResult.Count)

	entries2, _, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.Len(t, entries2, 2)
}

func TestPackInvalidPath(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	_, err = s.Pack(
		dir,
		[]string{"../../../etc/passwd"},
		"/tmp/sprync-evil.tar.gz",
		true,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestDeleteInvalidPath(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	_, err = s.Delete(dir, []string{"../../../etc/passwd"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestPackBadDest(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	_, err = s.Pack(
		dir,
		[]string{"a.go"},
		"/etc/evil.tar.gz",
		true,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "/tmp/")
}

func TestManifestLargeTree(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	files := map[string]string{}
	for i := range 200 {
		name := filepath.Join(
			"pkg",
			string(rune('a'+i%26)),
			filepath.Base(
				t.TempDir(),
			)+".go",
		)
		_ = name
	}
	for i := range 200 {
		subdir := string(rune('a' + i%26))
		name := subdir + "/" +
			"file" + string(rune('0'+i%10)) +
			string(rune('0'+i/10%10)) + ".go"
		files[name] = "package " + subdir
	}
	makeTree(t, dir, files)

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, len(files))
}

func TestQuit(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)

	err = s.Quit()
	assert.NoError(t, err)
}
