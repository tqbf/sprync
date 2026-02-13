package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tqbf/sprync/pkg/pack"
	"github.com/tqbf/sprync/pkg/paths"
	"github.com/tqbf/sprync/pkg/protocol"
)

const version = "0.1.0"

var trackedFiles []string

type sender func(protocol.Response)

func (s sender) fatal(msg string) {
	s(protocol.Response{
		Type:    protocol.TypeError,
		Message: msg,
		Fatal:   true,
	})
}

func (s sender) nonFatal(msg string) {
	s(protocol.Response{
		Type:    protocol.TypeError,
		Message: msg,
	})
}

func main() {
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stderr, nil),
	))

	enc := json.NewEncoder(os.Stdout)
	send := sender(func(resp protocol.Response) {
		if err := enc.Encode(resp); err != nil {
			slog.Error("write response", "err", err)
			cleanup()
			os.Exit(1)
		}
	})

	send(protocol.Response{
		Type:    protocol.TypeReady,
		Version: version,
		PID:     os.Getpid(),
	})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 16<<20), 16<<20)

	for scanner.Scan() {
		req, err := protocol.ParseRequest(scanner.Bytes())
		if err != nil {
			send.fatal(fmt.Sprintf("parse: %s", err))
			continue
		}

		switch req.Cmd {
		case "manifest":
			handleManifest(req, send)
		case "pack":
			handlePack(req, send)
		case "extract":
			handleExtract(req, send)
		case "delete":
			handleDelete(req, send)
		case "transfer":
			handleTransfer(req, send)
		case "quit":
			cleanup()
			os.Exit(0)
		default:
			send.fatal(
				fmt.Sprintf("unknown command: %s", req.Cmd),
			)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Error("stdin read", "err", err)
	}
	cleanup()
}

func cleanup() {
	for _, f := range trackedFiles {
		os.Remove(f)
	}
}

func handleManifest(req *protocol.Request, send sender) {
	start := time.Now()

	info, err := os.Stat(req.Dir)
	if err != nil || !info.IsDir() {
		send(protocol.Response{
			Type:   protocol.TypeManifestDone,
			Exists: protocol.BoolPtr(false),
		})
		return
	}

	matcher := paths.NewExcludeMatcher(req.Excludes)
	count := 0
	buf := make([]byte, 1<<20)

	walkErr := filepath.WalkDir(
		req.Dir,
		func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				send.nonFatal(fmt.Sprintf("walk: %s", err))
				return nil
			}

			rel, err := filepath.Rel(req.Dir, p)
			if err != nil {
				return nil
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
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}

			entry, err := hashFileEntry(p, rel, buf)
			if err != nil {
				send.nonFatal(
					fmt.Sprintf("hash %s: %s", rel, err),
				)
				return nil
			}

			send(protocol.Response{
				Type: protocol.TypeEntry,
				Path: entry.path,
				Hash: entry.hash,
				Mode: entry.mode,
				Size: entry.size,
			})
			count++
			return nil
		},
	)

	if walkErr != nil {
		send.fatal(fmt.Sprintf("walk failed: %s", walkErr))
		return
	}

	send(protocol.Response{
		Type:      protocol.TypeManifestDone,
		Count:     count,
		Exists:    protocol.BoolPtr(true),
		ElapsedMs: time.Since(start).Milliseconds(),
	})
}

type fileEntry struct {
	path string
	hash string
	mode int
	size int64
}

func hashFileEntry(
	absPath, relPath string, buf []byte,
) (fileEntry, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return fileEntry{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fileEntry{}, err
	}

	h := sha256.New()
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return fileEntry{}, err
	}

	return fileEntry{
		path: relPath,
		hash: hex.EncodeToString(h.Sum(nil)),
		mode: int(info.Mode().Perm()),
		size: info.Size(),
	}, nil
}

func handlePack(req *protocol.Request, send sender) {
	if err := validateDir(req.Dir); err != nil {
		send.fatal(err.Error())
		return
	}
	if err := validatePaths(req.Paths); err != nil {
		send.fatal(err.Error())
		return
	}
	if !validTmpPath(req.Dest) {
		send.fatal("dest must be under /tmp/")
		return
	}

	for _, p := range req.Paths {
		full := filepath.Join(req.Dir, p)
		if !paths.IsWithinDir(req.Dir, full) {
			send.fatal(
				fmt.Sprintf("path escapes dir: %s", p),
			)
			return
		}
	}

	trackedFiles = append(trackedFiles, req.Dest)

	f, err := os.Create(req.Dest)
	if err != nil {
		send.fatal(fmt.Sprintf("create dest: %s", err))
		return
	}

	count, err := pack.PackTar(
		req.Dir, req.Paths, f, req.Compress,
	)
	f.Close()
	if err != nil {
		os.Remove(req.Dest)
		send.fatal(fmt.Sprintf("pack: %s", err))
		return
	}

	info, err := os.Stat(req.Dest)
	if err != nil {
		send.fatal(fmt.Sprintf("stat dest: %s", err))
		return
	}

	send(protocol.Response{
		Type:  protocol.TypePackDone,
		Dest:  req.Dest,
		Size:  info.Size(),
		Count: count,
	})
}

func handleExtract(req *protocol.Request, send sender) {
	if req.Dir == "" {
		send.fatal("missing dir")
		return
	}
	if !validTmpPath(req.Src) {
		send.fatal("src must be under /tmp/")
		return
	}

	f, err := os.Open(req.Src)
	if err != nil {
		send.fatal(fmt.Sprintf("open src: %s", err))
		return
	}

	count, err := pack.UnpackTar(f, req.Dir, req.Compress)
	f.Close()
	if err != nil {
		send.fatal(fmt.Sprintf("extract: %s", err))
		return
	}

	os.Remove(req.Src)

	send(protocol.Response{
		Type:  protocol.TypeExtractDone,
		Count: count,
	})
}

func handleDelete(req *protocol.Request, send sender) {
	if err := validateDir(req.Dir); err != nil {
		send.fatal(err.Error())
		return
	}
	for _, p := range req.Paths {
		if err := paths.ValidateRelPath(p); err != nil {
			send.fatal(fmt.Sprintf("invalid path: %s", err))
			return
		}
		full := filepath.Join(req.Dir, p)
		if !paths.IsWithinDir(req.Dir, full) {
			send.fatal(fmt.Sprintf("path escapes dir: %s", p))
			return
		}
	}

	count := 0
	for _, p := range req.Paths {
		full := filepath.Join(req.Dir, p)
		if err := os.RemoveAll(full); err != nil {
			send.nonFatal(
				fmt.Sprintf("delete %s: %s", p, err),
			)
			continue
		}
		count++
	}

	send(protocol.Response{
		Type:  protocol.TypeDeleteDone,
		Count: count,
	})
}

func validateDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("missing dir")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("dir not found: %s", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	return nil
}

func validatePaths(pathList []string) error {
	for _, p := range pathList {
		if err := paths.ValidateRelPath(p); err != nil {
			return fmt.Errorf("invalid path: %w", err)
		}
	}
	return nil
}

func validTmpPath(p string) bool {
	return p != "" && strings.HasPrefix(p, "/tmp/")
}

func handleTransfer(
	req *protocol.Request, send sender,
) {
	if err := validateDir(req.Dir); err != nil {
		send.fatal(err.Error())
		return
	}
	if err := validatePaths(req.Paths); err != nil {
		send.fatal(err.Error())
		return
	}
	for _, p := range req.Paths {
		full := filepath.Join(req.Dir, p)
		if !paths.IsWithinDir(req.Dir, full) {
			send.fatal(
				fmt.Sprintf("path escapes dir: %s", p),
			)
			return
		}
	}
	if req.URL == "" {
		send.fatal("missing url")
		return
	}
	if req.Token == "" {
		send.fatal("missing token")
		return
	}

	pr, pw := io.Pipe()

	type packResult struct {
		count int
		err   error
	}
	ch := make(chan packResult, 1)

	go func() {
		count, err := pack.PackTar(
			req.Dir, req.Paths, pw, req.Compress,
		)
		pw.CloseWithError(err)
		ch <- packResult{count, err}
	}()

	cr := &countingReader{r: pr}
	httpReq, err := http.NewRequest("PUT", req.URL, cr)
	if err != nil {
		pr.Close()
		send.fatal(fmt.Sprintf("build request: %s", err))
		return
	}
	httpReq.Header.Set(
		"Authorization", "Bearer "+req.Token,
	)
	httpReq.Header.Set(
		"Content-Type", "application/octet-stream",
	)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		send.fatal(fmt.Sprintf("transfer: %s", err))
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		send.fatal(
			fmt.Sprintf(
				"transfer http %d", resp.StatusCode,
			),
		)
		return
	}

	res := <-ch
	if res.err != nil {
		send.fatal(fmt.Sprintf("pack: %s", res.err))
		return
	}

	dest := extractPathParam(req.URL)

	send(protocol.Response{
		Type:  protocol.TypeTransferDone,
		Count: res.count,
		Size:  cr.n,
		Dest:  dest,
	})
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func extractPathParam(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("path")
}
