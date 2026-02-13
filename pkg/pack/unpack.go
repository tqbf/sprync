package pack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func UnpackTar(
	r io.Reader,
	dir string,
	compress bool,
) (int, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return 0, fmt.Errorf("create dir: %w", err)
	}

	var tr *tar.Reader
	if compress {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return 0, fmt.Errorf("gzip reader: %w", err)
		}
		defer gr.Close()
		tr = tar.NewReader(gr)
	} else {
		tr = tar.NewReader(r)
	}

	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, fmt.Errorf("read tar: %w", err)
		}

		name := filepath.Clean(hdr.Name)
		if err := validateTarPath(name); err != nil {
			return count, err
		}

		target := filepath.Join(dir, name)
		if !isWithinDir(dir, target) {
			return count, fmt.Errorf(
				"path escapes dir: %s", name,
			)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return count, fmt.Errorf(
					"mkdir %s: %w", name, err,
				)
			}
		case tar.TypeReg:
			if err := extractFile(tr, target, hdr); err != nil {
				return count, err
			}
			count++
		}
	}
	return count, nil
}

func extractFile(
	tr *tar.Reader, target string, hdr *tar.Header,
) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	f, err := os.OpenFile(
		target,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		os.FileMode(hdr.Mode&0777),
	)
	if err != nil {
		return fmt.Errorf("create %s: %w", hdr.Name, err)
	}

	_, copyErr := io.Copy(f, tr)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", hdr.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", hdr.Name, closeErr)
	}
	return nil
}

func validateTarPath(name string) error {
	if name == "" || name == "." {
		return nil
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("absolute path in tar: %s", name)
	}
	if strings.Contains(name, "..") {
		parts := strings.Split(
			filepath.ToSlash(name), "/",
		)
		for _, p := range parts {
			if p == ".." {
				return fmt.Errorf(
					"path traversal in tar: %s", name,
				)
			}
		}
	}
	return nil
}

func isWithinDir(dir, full string) bool {
	rel, err := filepath.Rel(dir, full)
	if err != nil {
		return false
	}
	return rel != ".." &&
		!strings.HasPrefix(rel, "../") &&
		!filepath.IsAbs(rel)
}
