package pack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(dir, path)
		assert.NoError(t, os.MkdirAll(filepath.Dir(full), 0755))
		assert.NoError(t, os.WriteFile(full, []byte(content), 0644))
	}
}

func TestWalkLocal(t *testing.T) {
	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"main.go":          "package main",
		"src/util.go":      "package src",
		"node_modules/a.js": "module",
		"test.pyc":         "bytecode",
	})

	m, err := WalkLocal(dir, []string{
		"node_modules", "*.pyc",
	})
	assert.NoError(t, err)
	assert.Len(t, m, 2)
	assert.Contains(t, m, "main.go")
	assert.Contains(t, m, "src/util.go")
	assert.NotContains(t, m, "node_modules/a.js")
	assert.NotContains(t, m, "test.pyc")
}

func TestWalkLocalHashes(t *testing.T) {
	dir := t.TempDir()
	makeTree(t, dir, map[string]string{
		"a.txt": "hello",
		"b.txt": "hello",
		"c.txt": "world",
	})

	m, err := WalkLocal(dir, nil)
	assert.NoError(t, err)
	assert.Equal(t, m["a.txt"].Hash, m["b.txt"].Hash)
	assert.NotEqual(t, m["a.txt"].Hash, m["c.txt"].Hash)
}

func TestWalkLocalEmpty(t *testing.T) {
	dir := t.TempDir()
	m, err := WalkLocal(dir, nil)
	assert.NoError(t, err)
	assert.Len(t, m, 0)
}

func TestComputeDiff(t *testing.T) {
	local := Manifest{
		"new.go": {
			Path: "new.go", Hash: "aaa", Size: 100,
		},
		"changed.go": {
			Path: "changed.go", Hash: "bbb", Size: 200,
		},
		"same.go": {
			Path: "same.go", Hash: "ccc", Size: 300,
		},
	}
	remote := Manifest{
		"changed.go": {
			Path: "changed.go", Hash: "old", Size: 200,
		},
		"same.go": {
			Path: "same.go", Hash: "ccc", Size: 300,
		},
		"deleted.go": {
			Path: "deleted.go", Hash: "ddd", Size: 400,
		},
	}

	diff := ComputeDiff(local, remote, true)
	assert.Equal(t,
		[]string{"changed.go", "new.go"},
		diff.Uploads,
	)
	assert.Equal(t,
		[]string{"deleted.go"},
		diff.Deletes,
	)

	diffNoDelete := ComputeDiff(local, remote, false)
	assert.Nil(t, diffNoDelete.Deletes)
}

func TestComputeDiffIdentical(t *testing.T) {
	m := Manifest{
		"a.go": {Path: "a.go", Hash: "x"},
	}
	diff := ComputeDiff(m, m, true)
	assert.Nil(t, diff.Uploads)
	assert.Nil(t, diff.Deletes)
}

func TestComputeDiffDisjoint(t *testing.T) {
	local := Manifest{
		"a.go": {Path: "a.go", Hash: "x"},
	}
	remote := Manifest{
		"b.go": {Path: "b.go", Hash: "y"},
	}
	diff := ComputeDiff(local, remote, true)
	assert.Equal(t, []string{"a.go"}, diff.Uploads)
	assert.Equal(t, []string{"b.go"}, diff.Deletes)
}
