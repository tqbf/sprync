package pack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tqbf/sprync/pkg/paths"
)

func PackTar(
	dir string,
	filePaths []string,
	w io.Writer,
	compress bool,
) (int, error) {
	var tw *tar.Writer
	if compress {
		gw := gzip.NewWriter(w)
		defer gw.Close()
		tw = tar.NewWriter(gw)
	} else {
		tw = tar.NewWriter(w)
	}
	defer tw.Close()

	dirs := collectDirs(filePaths)
	for _, d := range dirs {
		err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     d + "/",
			Mode:     0755,
			ModTime:  time.Time{},
		})
		if err != nil {
			return 0, fmt.Errorf("write dir header: %w", err)
		}
	}

	count := 0
	for _, rel := range filePaths {
		if err := paths.ValidateRelPath(rel); err != nil {
			return 0, fmt.Errorf("invalid path %s: %w", rel, err)
		}
		abs := filepath.Join(dir, rel)
		if !paths.IsWithinDir(dir, abs) {
			return 0, fmt.Errorf("path escapes dir: %s", rel)
		}

		if err := addFileToTar(tw, abs, rel); err != nil {
			return 0, err
		}
		count++
	}

	return count, nil
}

func addFileToTar(
	tw *tar.Writer,
	absPath, relPath string,
) error {
	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", relPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat %s: %w", relPath, err)
	}

	hdr := &tar.Header{
		Name:    relPath,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: time.Time{},
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write header %s: %w", relPath, err)
	}
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("write body %s: %w", relPath, err)
	}
	return nil
}

func collectDirs(filePaths []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, p := range filePaths {
		dir := filepath.Dir(p)
		if dir == "." {
			continue
		}
		parts := strings.Split(filepath.ToSlash(dir), "/")
		var b strings.Builder
		for i, part := range parts {
			if i > 0 {
				b.WriteString("/")
			}
			b.WriteString(part)
			d := b.String()
			if !seen[d] {
				seen[d] = true
				result = append(result, d)
			}
		}
	}
	return result
}
