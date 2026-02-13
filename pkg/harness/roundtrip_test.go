package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tqbf/sprync/pkg/pack"
)

func TestPushFlowNormalSync(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	makeTree(t, localDir, map[string]string{
		"main.go":     "package main\nfunc main() {}",
		"util.go":     "package main\nfunc util() {}",
		"src/lib.go":  "package src",
		"new_file.go": "package main\nfunc new() {}",
	})
	makeTree(t, remoteDir, map[string]string{
		"main.go":    "package main\nfunc main() {}",
		"util.go":    "package main\nfunc oldutil() {}",
		"src/lib.go": "package src",
		"old.go":     "package main\nfunc old() {}",
	})

	entries, exists, _, err := s.Manifest(remoteDir, nil)
	require.NoError(t, err)
	assert.True(t, exists)

	remoteManifest := make(pack.Manifest, len(entries))
	for _, e := range entries {
		remoteManifest[e.Path] = e
	}

	localManifest, err := pack.WalkLocal(localDir, nil)
	require.NoError(t, err)

	diff := pack.ComputeDiff(
		localManifest, remoteManifest, true,
	)

	assert.Contains(t, diff.Uploads, "new_file.go")
	assert.Contains(t, diff.Uploads, "util.go")
	assert.NotContains(t, diff.Uploads, "main.go")
	assert.NotContains(t, diff.Uploads, "src/lib.go")
	assert.Equal(t, []string{"old.go"}, diff.Deletes)

	tarPath := "/tmp/sprync-push-test.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	count, err := pack.PackTar(
		localDir, diff.Uploads, f, true,
	)
	f.Close()
	require.NoError(t, err)
	assert.Equal(t, len(diff.Uploads), count)
	defer os.Remove(tarPath)

	extractResult, err := s.Extract(
		remoteDir, tarPath, true,
	)
	require.NoError(t, err)
	assert.Equal(t, len(diff.Uploads), extractResult.Count)

	delResult, err := s.Delete(remoteDir, diff.Deletes)
	require.NoError(t, err)
	assert.Equal(t, 1, delResult.Count)

	_, err = os.Stat(
		filepath.Join(remoteDir, "new_file.go"),
	)
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(remoteDir, "old.go"))
	assert.True(t, os.IsNotExist(err))

	got, err := os.ReadFile(
		filepath.Join(remoteDir, "util.go"),
	)
	require.NoError(t, err)
	assert.Equal(t, "package main\nfunc util() {}", string(got))

	t.Logf("uploads: %v", diff.Uploads)
	t.Logf("deletes: %v", diff.Deletes)
}

func TestPushFlowBlindPush(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	localDir := t.TempDir()
	nonexistent := filepath.Join(t.TempDir(), "newproject")

	makeTree(t, localDir, map[string]string{
		"main.go":    "package main",
		"src/lib.go": "package src",
	})

	entries, exists, _, err := s.Manifest(nonexistent, nil)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)

	files, err := collectLocalPaths(localDir, nil)
	require.NoError(t, err)
	assert.Len(t, files, 2)

	tarPath := "/tmp/sprync-blind-test.tar.gz"
	localManifest, err := pack.WalkLocal(localDir, nil)
	require.NoError(t, err)
	var allPaths []string
	for p := range localManifest {
		allPaths = append(allPaths, p)
	}

	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(localDir, allPaths, f, true)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	extractResult, err := s.Extract(
		nonexistent, tarPath, true,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, extractResult.Count)

	_, err = os.Stat(
		filepath.Join(nonexistent, "main.go"),
	)
	assert.NoError(t, err)
	_, err = os.Stat(
		filepath.Join(nonexistent, "src/lib.go"),
	)
	assert.NoError(t, err)

	t.Logf("blind push: %d files", len(files))
}

func TestPullFlow(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	makeTree(t, localDir, map[string]string{
		"main.go": "package main\nfunc main() {}",
	})
	makeTree(t, remoteDir, map[string]string{
		"main.go":     "package main\nfunc main() {}",
		"new_file.go": "package main\nfunc new() {}",
	})

	entries, exists, _, err := s.Manifest(remoteDir, nil)
	require.NoError(t, err)
	assert.True(t, exists)

	remoteManifest := make(pack.Manifest, len(entries))
	for _, e := range entries {
		remoteManifest[e.Path] = e
	}

	localManifest, err := pack.WalkLocal(localDir, nil)
	require.NoError(t, err)

	var downloads []string
	var deletes []string
	for path, re := range remoteManifest {
		le, exists := localManifest[path]
		if !exists || le.Hash != re.Hash {
			downloads = append(downloads, path)
		}
	}
	for path := range localManifest {
		if _, exists := remoteManifest[path]; !exists {
			deletes = append(deletes, path)
		}
	}

	assert.Contains(t, downloads, "new_file.go")
	assert.NotContains(t, downloads, "main.go")
	assert.Len(t, deletes, 0)

	if len(downloads) > 0 {
		packResult, err := s.Pack(
			remoteDir,
			downloads,
			"/tmp/sprync-pull.tar.gz",
			true,
		)
		require.NoError(t, err)
		assert.Equal(t, len(downloads), packResult.Count)
		assert.True(t, packResult.Size > 0)

		tarRC, err := os.Open(packResult.Dest)
		require.NoError(t, err)
		count, err := pack.UnpackTar(
			tarRC, localDir, true,
		)
		tarRC.Close()
		require.NoError(t, err)
		assert.Equal(t, len(downloads), count)
	}

	got, err := os.ReadFile(
		filepath.Join(localDir, "new_file.go"),
	)
	require.NoError(t, err)
	assert.Equal(t,
		"package main\nfunc new() {}", string(got),
	)

	t.Logf("downloads: %v", downloads)
	t.Logf("deletes: %v", deletes)
}

func TestAlreadyInSync(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"main.go": "package main",
		"lib.go":  "package main",
	})

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)

	remoteManifest := make(pack.Manifest, len(entries))
	for _, e := range entries {
		remoteManifest[e.Path] = e
	}

	localManifest, err := pack.WalkLocal(dir, nil)
	require.NoError(t, err)

	diff := pack.ComputeDiff(
		localManifest, remoteManifest, true,
	)

	assert.Nil(t, diff.Uploads)
	assert.Nil(t, diff.Deletes)
	t.Log("already in sync")
}

func collectLocalPaths(
	dir string,
	excludes []string,
) ([]string, error) {
	m, err := pack.WalkLocal(dir, excludes)
	if err != nil {
		return nil, err
	}
	var paths []string
	for p := range m {
		paths = append(paths, p)
	}
	return paths, nil
}

func TestPullFromNonexistent(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	entries, exists, _, err := s.Manifest(
		"/nonexistent/dir", nil,
	)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)
}

func TestDiffWithExcludes(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	localDir := t.TempDir()
	remoteDir := t.TempDir()

	makeTree(t, localDir, map[string]string{
		"main.go":           "package main",
		"node_modules/a.js": "module",
	})
	makeTree(t, remoteDir, map[string]string{
		"main.go":           "package main",
		"node_modules/b.js": "other module",
	})

	excludes := []string{"node_modules"}

	entries, exists, _, err := s.Manifest(
		remoteDir, excludes,
	)
	require.NoError(t, err)
	assert.True(t, exists)

	remoteManifest := make(pack.Manifest, len(entries))
	for _, e := range entries {
		remoteManifest[e.Path] = e
	}
	assert.NotContains(t, remoteManifest, "node_modules/b.js")

	localManifest, err := pack.WalkLocal(
		localDir, excludes,
	)
	require.NoError(t, err)
	assert.NotContains(t, localManifest, "node_modules/a.js")

	diff := pack.ComputeDiff(
		localManifest, remoteManifest, true,
	)
	assert.Nil(t, diff.Uploads)
	assert.Nil(t, diff.Deletes)
}

func TestFileModePreserved(t *testing.T) {
	bin := buildSpryncd(t)
	s, err := Start(bin)
	require.NoError(t, err)
	defer s.Quit()

	dir := t.TempDir()
	f := filepath.Join(dir, "script.sh")
	require.NoError(t, os.WriteFile(
		f, []byte("#!/bin/sh"), 0755,
	))

	entries, exists, _, err := s.Manifest(dir, nil)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, 0755, entries[0].Mode)
}
