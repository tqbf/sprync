package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/tqbf/sprync/pkg/paths"
)

type fileJob struct {
	relPath string
	absPath string
}

type hashResult struct {
	entry ManifestEntry
	err   error
}

func WalkLocal(
	dir string,
	excludes []string,
) (Manifest, error) {
	matcher := paths.NewExcludeMatcher(excludes)

	var jobs []fileJob
	err := filepath.WalkDir(
		dir,
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if matcher.Match(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			jobs = append(jobs, fileJob{
				relPath: rel,
				absPath: p,
			})
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	workers := runtime.NumCPU()
	if workers > len(jobs) {
		workers = len(jobs)
	}
	if workers == 0 {
		return Manifest{}, nil
	}

	jobCh := make(chan fileJob, len(jobs))
	resultCh := make(chan hashResult, len(jobs))

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hashWorker(jobCh, resultCh)
		}()
	}

	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	wg.Wait()
	close(resultCh)

	manifest := make(Manifest, len(jobs))
	for r := range resultCh {
		if r.err != nil {
			return nil, r.err
		}
		manifest[r.entry.Path] = r.entry
	}
	return manifest, nil
}

func hashWorker(
	jobs <-chan fileJob,
	results chan<- hashResult,
) {
	buf := make([]byte, 1<<20)
	for j := range jobs {
		entry, err := hashFile(j.absPath, j.relPath, buf)
		results <- hashResult{entry, err}
	}
}

func hashFile(
	absPath, relPath string,
	buf []byte,
) (ManifestEntry, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return ManifestEntry{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ManifestEntry{}, err
	}

	h := sha256.New()
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return ManifestEntry{}, err
	}

	return ManifestEntry{
		Path: relPath,
		Hash: hex.EncodeToString(h.Sum(nil)),
		Mode: int(info.Mode().Perm()),
		Size: info.Size(),
	}, nil
}
