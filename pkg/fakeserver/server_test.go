package fakeserver

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tqbf/sprync/pkg/pack"
	"github.com/tqbf/sprync/pkg/protocol"
	"github.com/tqbf/sprync/pkg/spriteapi"
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

func setupServer(t *testing.T) (
	*Server,
	*spriteapi.Client,
	string,
) {
	t.Helper()
	rootDir := t.TempDir()
	srv := New(rootDir)
	t.Cleanup(srv.Close)

	client := spriteapi.New(
		srv.URL()+"/v1/sprites", "test-token",
	)
	client.HTTPClient = srv.HS.Client()

	return srv, client, rootDir
}

func openSession(
	t *testing.T,
	client *spriteapi.Client,
	spryncdBin string,
) *protocol.Session {
	t.Helper()
	ctx := context.Background()

	binary, err := os.ReadFile(spryncdBin)
	require.NoError(t, err)

	sess, err := protocol.OpenSession(
		ctx, client, "test-sprite", binary,
	)
	require.NoError(t, err)
	return sess
}

func TestWSSessionReady(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, _ := setupServer(t)

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(context.Background())

	assert.Equal(t, "0.1.0", sess.Version)
	assert.NotZero(t, sess.PID)
}

func TestWSManifestExistingDir(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))
	makeTree(t, remoteDir, map[string]string{
		"main.go":     "package main",
		"src/util.go": "package src",
		"README.md":   "hello",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(context.Background())

	entries, exists, elapsed, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 3)
	assert.True(t, elapsed >= 0)

	pathSet := map[string]bool{}
	for _, e := range entries {
		pathSet[e.Path] = true
		assert.NotEmpty(t, e.Hash)
		assert.NotZero(t, e.Mode)
	}
	assert.True(t, pathSet["main.go"])
	assert.True(t, pathSet["src/util.go"])
	assert.True(t, pathSet["README.md"])
}

func TestWSManifestNonexistentDir(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, _ := setupServer(t)

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(context.Background())

	entries, exists, _, err := sess.Manifest(
		"/nonexistent/path", nil,
	)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)
}

func TestWSManifestWithExcludes(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))
	makeTree(t, remoteDir, map[string]string{
		"main.go":           "package main",
		"node_modules/a.js": "module",
		"test.pyc":          "bytecode",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(context.Background())

	entries, exists, _, err := sess.Manifest(
		remoteDir, []string{"node_modules", "*.pyc"},
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
	assert.Equal(t, "main.go", entries[0].Path)
}

func TestWSFullPushFlow(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	localDir := t.TempDir()
	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	makeTree(t, localDir, map[string]string{
		"main.go":     "package main\nfunc main() {}",
		"util.go":     "package main\nfunc util() {}",
		"new_file.go": "package main\nfunc new() {}",
	})
	makeTree(t, remoteDir, map[string]string{
		"main.go": "package main\nfunc main() {}",
		"util.go": "package main\nfunc oldutil() {}",
		"old.go":  "package main\nfunc old() {}",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries, exists, _, err := sess.Manifest(remoteDir, nil)
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
	assert.Equal(t, []string{"old.go"}, diff.Deletes)

	tarPath := "/tmp/sprync-ws-push.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(
		localDir, diff.Uploads, f, true,
	)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	err = client.FSWrite(
		ctx, "test-sprite", tarPath, "0644", false,
		mustOpen(t, tarPath),
	)
	require.NoError(t, err)

	fsTarget := filepath.Join(rootDir, tarPath)
	_, err = os.Stat(fsTarget)
	require.NoError(t, err)

	extractResult, err := sess.Extract(
		remoteDir, tarPath, true,
	)
	require.NoError(t, err)
	assert.Equal(t, len(diff.Uploads), extractResult.Count)

	delResult, err := sess.Delete(remoteDir, diff.Deletes)
	require.NoError(t, err)
	assert.Equal(t, 1, delResult.Count)

	_, err = os.Stat(
		filepath.Join(remoteDir, "new_file.go"),
	)
	assert.NoError(t, err)
	_, err = os.Stat(
		filepath.Join(remoteDir, "old.go"),
	)
	assert.True(t, os.IsNotExist(err))

	got, err := os.ReadFile(
		filepath.Join(remoteDir, "util.go"),
	)
	require.NoError(t, err)
	assert.Equal(t,
		"package main\nfunc util() {}", string(got),
	)
}

func TestWSFullPullFlow(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	localDir := t.TempDir()
	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	makeTree(t, localDir, map[string]string{
		"main.go": "package main\nfunc main() {}",
	})
	makeTree(t, remoteDir, map[string]string{
		"main.go":     "package main\nfunc main() {}",
		"new_file.go": "package main\nfunc new() {}",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries, exists, _, err := sess.Manifest(remoteDir, nil)
	require.NoError(t, err)
	assert.True(t, exists)

	remoteManifest := make(pack.Manifest, len(entries))
	for _, e := range entries {
		remoteManifest[e.Path] = e
	}

	localManifest, err := pack.WalkLocal(localDir, nil)
	require.NoError(t, err)

	var downloads []string
	for path, re := range remoteManifest {
		le, ok := localManifest[path]
		if !ok || le.Hash != re.Hash {
			downloads = append(downloads, path)
		}
	}

	packResult, err := sess.Pack(
		remoteDir, downloads, true,
	)
	require.NoError(t, err)
	assert.Equal(t, len(downloads), packResult.Count)
	assert.True(t, packResult.Size > 0)

	rc, err := client.FSRead(
		ctx, "test-sprite", packResult.Dest,
	)
	require.NoError(t, err)
	count, err := pack.UnpackTar(rc, localDir, true)
	rc.Close()
	require.NoError(t, err)
	assert.Equal(t, len(downloads), count)

	got, err := os.ReadFile(
		filepath.Join(localDir, "new_file.go"),
	)
	require.NoError(t, err)
	assert.Equal(t,
		"package main\nfunc new() {}", string(got),
	)
}

func TestWSBlindPush(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	localDir := t.TempDir()
	remoteDir := filepath.Join(rootDir, "newproject")

	makeTree(t, localDir, map[string]string{
		"main.go":    "package main",
		"src/lib.go": "package src",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries, exists, _, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Len(t, entries, 0)

	localManifest, err := pack.WalkLocal(localDir, nil)
	require.NoError(t, err)
	var allPaths []string
	for p := range localManifest {
		allPaths = append(allPaths, p)
	}
	sort.Strings(allPaths)

	tarPath := "/tmp/sprync-ws-blind.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(localDir, allPaths, f, true)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	err = client.FSWrite(
		ctx, "test-sprite", tarPath, "0644", false,
		mustOpen(t, tarPath),
	)
	require.NoError(t, err)

	extractResult, err := sess.Extract(
		remoteDir, tarPath, true,
	)
	require.NoError(t, err)
	assert.Equal(t, 2, extractResult.Count)

	_, err = os.Stat(
		filepath.Join(remoteDir, "main.go"),
	)
	assert.NoError(t, err)
	_, err = os.Stat(
		filepath.Join(remoteDir, "src/lib.go"),
	)
	assert.NoError(t, err)
}

func TestWSDeleteFlow(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))
	makeTree(t, remoteDir, map[string]string{
		"keep.go":   "keep",
		"remove.go": "remove",
		"sub/old.go": "old",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	delResult, err := sess.Delete(
		remoteDir, []string{"remove.go", "sub/old.go"},
	)
	require.NoError(t, err)
	assert.Equal(t, 2, delResult.Count)

	_, err = os.Stat(
		filepath.Join(remoteDir, "remove.go"),
	)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(
		filepath.Join(remoteDir, "sub/old.go"),
	)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(
		filepath.Join(remoteDir, "keep.go"),
	)
	assert.NoError(t, err)
}

func TestWSMultipleOperations(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))
	makeTree(t, remoteDir, map[string]string{
		"a.go":  "package a",
		"b.go":  "package b",
		"c.txt": "text",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries1, exists, _, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries1, 3)

	packResult, err := sess.Pack(
		remoteDir, []string{"a.go"}, true,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, packResult.Count)

	delResult, err := sess.Delete(
		remoteDir, []string{"c.txt"},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, delResult.Count)

	entries2, _, _, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.Len(t, entries2, 2)
}

func TestWSSessionClose(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, _ := setupServer(t)
	ctx := context.Background()

	sess := openSession(t, client, spryncdBin)

	err := sess.Close(ctx)
	assert.NoError(t, err)
}

func TestWSUploadAndRunSpryncd(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	binary, err := os.ReadFile(spryncdBin)
	require.NoError(t, err)

	err = client.FSWrite(
		ctx, "test-sprite",
		"/tmp/spryncd", "0755", false,
		bytes.NewReader(binary),
	)
	require.NoError(t, err)

	uploaded := filepath.Join(rootDir, "tmp", "spryncd")
	info, err := os.Stat(uploaded)
	require.NoError(t, err)
	assert.Equal(t, len(binary), int(info.Size()))
	assert.True(t, info.Mode()&0111 != 0)

	sess, err := protocol.OpenSession(
		ctx, client, "test-sprite", binary,
	)
	require.NoError(t, err)
	defer sess.Close(ctx)

	assert.Equal(t, "0.1.0", sess.Version)
}

func TestWSFSWriteAndRead(t *testing.T) {
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	content := []byte("hello world from sprync")
	err := client.FSWrite(
		ctx, "test-sprite",
		"/tmp/test-file.txt", "0644", false,
		bytes.NewReader(content),
	)
	require.NoError(t, err)

	written := filepath.Join(rootDir, "tmp", "test-file.txt")
	got, err := os.ReadFile(written)
	require.NoError(t, err)
	assert.Equal(t, content, got)

	rc, err := client.FSRead(
		ctx, "test-sprite", "/tmp/test-file.txt",
	)
	require.NoError(t, err)
	var buf bytes.Buffer
	buf.ReadFrom(rc)
	rc.Close()
	assert.Equal(t, content, buf.Bytes())
}

func TestWSLargeManifest(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "bigproject")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	files := map[string]string{}
	for i := range 500 {
		subdir := string(rune('a' + i%26))
		name := filepath.Join(
			subdir,
			filepath.Base(t.TempDir())+".go",
		)
		files[name] = "package " + subdir
	}
	makeTree(t, remoteDir, files)

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries, exists, _, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, len(files))
}

func TestWSPackPathTraversal(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	_, err := sess.Pack(
		remoteDir,
		[]string{"../../../etc/passwd"},
		true,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestWSDeletePathTraversal(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	_, err := sess.Delete(
		remoteDir, []string{"../../../etc/passwd"},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "escapes")
}

func TestWSRecoveryAfterFatalError(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))
	makeTree(t, remoteDir, map[string]string{
		"a.go": "package a",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	_, err := sess.Delete(
		remoteDir, []string{"../evil"},
	)
	assert.Error(t, err)

	entries, exists, _, err := sess.Manifest(
		remoteDir, nil,
	)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, entries, 1)
}

func TestWSHashConsistency(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	dir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(dir, 0755))
	makeTree(t, dir, map[string]string{
		"a.go": "package a",
		"b.go": "package b",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries1, _, _, err := sess.Manifest(dir, nil)
	require.NoError(t, err)

	entries2, _, _, err := sess.Manifest(dir, nil)
	require.NoError(t, err)

	sort.Slice(entries1, func(i, j int) bool {
		return entries1[i].Path < entries1[j].Path
	})
	sort.Slice(entries2, func(i, j int) bool {
		return entries2[i].Path < entries2[j].Path
	})

	assert.Equal(t, entries1, entries2)

	localManifest, err := pack.WalkLocal(dir, nil)
	require.NoError(t, err)

	for _, e := range entries1 {
		local, ok := localManifest[e.Path]
		assert.True(t, ok)
		assert.Equal(t, local.Hash, e.Hash)
	}
}

func TestWSExtractIntoNonexistentDir(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	makeTree(t, srcDir, map[string]string{
		"hello.go": "package hello",
	})

	tarPath := "/tmp/sprync-ws-extract-new.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(
		srcDir, []string{"hello.go"}, f, true,
	)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	err = client.FSWrite(
		ctx, "test-sprite", tarPath, "0644", false,
		mustOpen(t, tarPath),
	)
	require.NoError(t, err)

	newDir := filepath.Join(rootDir, "brand-new-dir")
	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	result, err := sess.Extract(newDir, tarPath, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)

	got, err := os.ReadFile(
		filepath.Join(newDir, "hello.go"),
	)
	require.NoError(t, err)
	assert.Equal(t, "package hello", string(got))
}

func TestWSPackWithNoFiles(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	result, err := sess.Pack(remoteDir, []string{}, true)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Count)
}

func TestWSDeleteNonexistentFile(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	remoteDir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(remoteDir, 0755))

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	result, err := sess.Delete(
		remoteDir, []string{"nonexistent.go"},
	)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)
}

func TestWSFileModePreservation(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	srcDir := t.TempDir()
	scriptPath := filepath.Join(srcDir, "run.sh")
	require.NoError(t, os.WriteFile(
		scriptPath, []byte("#!/bin/sh\necho hello"), 0755,
	))

	tarPath := "/tmp/sprync-ws-mode.tar.gz"
	f, err := os.Create(tarPath)
	require.NoError(t, err)
	_, err = pack.PackTar(
		srcDir, []string{"run.sh"}, f, true,
	)
	f.Close()
	require.NoError(t, err)
	defer os.Remove(tarPath)

	err = client.FSWrite(
		ctx, "test-sprite", tarPath, "0644", false,
		mustOpen(t, tarPath),
	)
	require.NoError(t, err)

	destDir := filepath.Join(rootDir, "mode-test")
	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	result, err := sess.Extract(destDir, tarPath, true)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Count)

	info, err := os.Stat(
		filepath.Join(destDir, "run.sh"),
	)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestWSSecondSyncNoOp(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, rootDir := setupServer(t)
	ctx := context.Background()

	dir := filepath.Join(rootDir, "project")
	require.NoError(t, os.MkdirAll(dir, 0755))
	makeTree(t, dir, map[string]string{
		"main.go": "package main",
		"lib.go":  "package lib",
	})

	sess := openSession(t, client, spryncdBin)
	defer sess.Close(ctx)

	entries, _, _, err := sess.Manifest(dir, nil)
	require.NoError(t, err)

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
}

func TestWSSessionTimeout(t *testing.T) {
	spryncdBin := buildSpryncd(t)
	_, client, _ := setupServer(t)

	sess := openSession(t, client, spryncdBin)

	ctx, cancel := context.WithTimeout(
		context.Background(), 100*time.Millisecond,
	)
	defer cancel()

	sess.Close(ctx)
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { f.Close() })
	return f
}
