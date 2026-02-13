package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tqbf/sprync/pkg/pack"
)

func TestManifestEmptyFile(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "empty.txt"), nil, 0644,
	))

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, "empty.txt", entries[0].Path)
	assert.Equal(t, int64(0), entries[0].Size)

	emptyHash := sha256.Sum256(nil)
	assert.Equal(t,
		hex.EncodeToString(emptyHash[:]),
		entries[0].Hash,
	)
}

func TestManifestBinaryContent(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "binary.dat"), data, 0644,
	))

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, int64(256), entries[0].Size)
	assert.NotEmpty(t, entries[0].Hash)
}

func TestManifestUnicodeFilenames(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"日本語.txt":       "japanese",
		"café.txt":       "french",
		"données/fête.md": "nested unicode",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 3)

	pathSet := map[string]bool{}
	for _, e := range entries {
		pathSet[e.Path] = true
	}
	assert.True(t, pathSet["日本語.txt"])
	assert.True(t, pathSet["café.txt"])
	assert.True(t, pathSet["données/fête.md"])
}

func TestManifestSpacesInFilenames(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"my file.go":             "package main",
		"my dir/another file.go": "package mydir",
		"  leading.txt":          "leading spaces",
		"trailing  .txt":         "trailing spaces",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 4)
}

func TestManifestDeeplyNested(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	parts := make([]string, 20)
	for i := range parts {
		parts[i] = "d"
	}
	deepPath := strings.Join(parts, "/") + "/deep.txt"
	makeTree(t, dir, map[string]string{
		deepPath: "deep content",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, deepPath, entries[0].Path)
}

func TestManifestVariousModes(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	files := map[string]os.FileMode{
		"readonly.txt":  0444,
		"private.key":   0600,
		"executable.sh": 0755,
		"normal.txt":    0644,
		"world.txt":     0666,
	}
	for name, mode := range files {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(
			p, []byte("content"), 0644,
		))
		require.NoError(t, os.Chmod(p, mode))
	}

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, len(files))

	byPath := map[string]int{}
	for _, e := range entries {
		byPath[e.Path] = e.Mode
	}
	for name, mode := range files {
		assert.Equal(t,
			int(mode), byPath[name],
			"mode mismatch for %s", name,
		)
	}
}

func TestManifestSkipsSymlinks(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"real.txt": "real content",
	})
	os.Symlink(
		filepath.Join(dir, "real.txt"),
		filepath.Join(dir, "link.txt"),
	)

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, "real.txt", entries[0].Path)
}

func TestManifestSkipsSubdirSymlinks(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	other := t.TempDir()
	makeTree(t, dir, map[string]string{
		"real.txt": "real",
	})
	makeTree(t, other, map[string]string{
		"secret.txt": "should not appear",
	})
	os.Symlink(other, filepath.Join(dir, "linked_dir"))

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
}

func TestManifestLargeFile(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	data := make([]byte, 2<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "big.bin"), data, 0644,
	))

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, int64(2<<20), entries[0].Size)

	h := sha256.Sum256(data)
	assert.Equal(t,
		hex.EncodeToString(h[:]),
		entries[0].Hash,
	)
}

func TestManifestOnFile(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	f := filepath.Join(t.TempDir(), "notadir.txt")
	require.NoError(t, os.WriteFile(f, []byte("x"), 0644))

	entries, exists, _, err := s.Manifest(f, nil)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)
}

func TestManifestEmptyDirString(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	entries, exists, _, err := s.Manifest("", nil)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)
}

func TestRecoveryAfterFatalError(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	_, err = s.Pack(
		dir,
		[]string{"../evil"},
		"/tmp/sprync-evil.tar.gz",
		false,
	)
	assert.Error(t, err)

	makeTree(t, dir, map[string]string{
		"ok.txt": "recovery test",
	})
	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
}

func TestRecoveryAfterMultipleFatalErrors(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()

	_, err = s.Pack(dir, []string{"../a"}, "/tmp/x.tar.gz", false)
	assert.Error(t, err)

	_, err = s.Delete(dir, []string{"../b"})
	assert.Error(t, err)

	_, err = s.Pack(dir, []string{"c"}, "/etc/bad.tar.gz", false)
	assert.Error(t, err)

	makeTree(t, dir, map[string]string{"ok.txt": "still alive"})
	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
}

func TestExtractBadSrc(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	_, err = s.Extract(t.TempDir(), "/etc/evil.tar.gz", true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "/tmp/")
}

func TestExtractEmptySrc(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	_, err = s.Extract(t.TempDir(), "", true)
	assert.Error(t, err)
}

func TestExtractEmptyDir(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	_, err = s.Extract("", "/tmp/x.tar.gz", true)
	assert.Error(t, err)
}

func TestDeleteEmptyPathsList(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	result, err := s.Delete(dir, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Count)
}

func TestPackEmptyPathsList(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	result, err := s.Pack(
		dir, nil, "/tmp/sprync-empty.tar.gz", true,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Count)
}

func TestManifestIdempotent(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.go": "package a",
		"b.go": "package b",
	})

	entries1, _, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	entries2, _, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)

	m1 := map[string]string{}
	for _, e := range entries1 {
		m1[e.Path] = e.Hash
	}
	m2 := map[string]string{}
	for _, e := range entries2 {
		m2[e.Path] = e.Hash
	}
	assert.Equal(t, m1, m2)
}

func TestLocalAndRemoteHashesAgree(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.go":         "package a\nfunc A() {}",
		"sub/b.go":     "package sub\nvar X = 1",
		"data/blob.bin": string([]byte{0, 1, 2, 3, 255}),
	})

	remoteEntries, _, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	remoteManifest := make(pack.Manifest, len(remoteEntries))
	for _, e := range remoteEntries {
		remoteManifest[e.Path] = e
	}

	localManifest, err := pack.WalkLocal(dir, nil)
	require.NoError(t, err)

	assert.Len(t, localManifest, len(remoteManifest))
	for path, local := range localManifest {
		remote, ok := remoteManifest[path]
		assert.True(t, ok, "missing remote: %s", path)
		assert.Equal(t, local.Hash, remote.Hash,
			"hash mismatch: %s", path)
		assert.Equal(t, local.Size, remote.Size,
			"size mismatch: %s", path)
		assert.Equal(t, local.Mode, remote.Mode,
			"mode mismatch: %s", path)
	}

	diff := pack.ComputeDiff(localManifest, remoteManifest, true)
	assert.Nil(t, diff.Uploads)
	assert.Nil(t, diff.Deletes)
}

func TestManifestExcludePatternInteraction(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"src/main.go":             "package main",
		"src/main_test.go":        "package main",
		"vendor/dep/dep.go":       "package dep",
		".git/config":             "git config",
		".git/objects/ab/cd":      "git obj",
		"build/output/bundle.js":  "bundle",
		"docs/README.md":          "docs",
		"src/.env":                "SECRET=x",
		"src/.env.local":          "LOCAL=y",
		"node_modules/pkg/idx.js": "module",
	})

	excludes := []string{
		".git",
		"vendor",
		"build/",
		"node_modules",
		".env",
		".env.*",
	}

	entries, exists, _, err := s.Manifest(dir, excludes)
	require.NoError(t, err)
	assert.True(t, exists)

	pathSet := map[string]bool{}
	for _, e := range entries {
		pathSet[e.Path] = true
	}

	assert.True(t, pathSet["src/main.go"])
	assert.True(t, pathSet["src/main_test.go"])
	assert.True(t, pathSet["docs/README.md"])

	assert.False(t, pathSet["vendor/dep/dep.go"])
	assert.False(t, pathSet[".git/config"])
	assert.False(t, pathSet[".git/objects/ab/cd"])
	assert.False(t, pathSet["build/output/bundle.js"])
	assert.False(t, pathSet["node_modules/pkg/idx.js"])
	assert.False(t, pathSet["src/.env"])
	assert.False(t, pathSet["src/.env.local"])
}
