package protocol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/tqbf/sprync/pkg/pack"
	"github.com/tqbf/sprync/pkg/spriteapi"
)

type Session struct {
	client     *spriteapi.Client
	sprite     string
	conn       *WSConn
	scanner    *bufio.Scanner
	mu         sync.Mutex
	remoteBin  string
	Version    string
	PID        int
}

func OpenSession(
	ctx context.Context,
	client *spriteapi.Client,
	sprite string,
	binary []byte,
) (*Session, error) {
	remoteBin := tmpPath("-spryncd")
	err := client.FSWrite(
		ctx, sprite, remoteBin, "0755", false,
		bytes.NewReader(binary),
	)
	if err != nil {
		return nil, err
	}

	ws, err := client.ExecWebSocket(
		ctx, sprite, []string{remoteBin}, true,
	)
	if err != nil {
		return nil, err
	}

	conn := NewWSConn(ctx, ws)

	go drainStderr(conn.Stderr())

	scanner := bufio.NewScanner(conn.Stdout())
	scanner.Buffer(make([]byte, 16<<20), 16<<20)

	s := &Session{
		client:    client,
		sprite:    sprite,
		conn:      conn,
		scanner:   scanner,
		remoteBin: remoteBin,
	}

	resp, err := s.readResponse()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.Type != TypeReady {
		conn.Close()
		return nil, fmt.Errorf(
			"expected ready, got %s", resp.Type,
		)
	}
	s.Version = resp.Version
	s.PID = resp.PID
	return s, nil
}

func drainStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		slog.Debug("spryncd stderr", "line", scanner.Text())
	}
}

func (s *Session) sendCmd(req Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return s.conn.WriteStdin(data)
}

func (s *Session) readResponse() (*Response, error) {
	if !s.scanner.Scan() {
		err := s.scanner.Err()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected EOF")
	}
	return ParseResponse(s.scanner.Bytes())
}

func (s *Session) Manifest(
	dir string,
	excludes []string,
) ([]pack.ManifestEntry, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.sendCmd(Request{
		Cmd:      "manifest",
		Dir:      dir,
		Excludes: excludes,
	})
	if err != nil {
		return nil, false, 0, err
	}

	var entries []pack.ManifestEntry
	for {
		resp, err := s.readResponse()
		if err != nil {
			return nil, false, 0, err
		}
		switch resp.Type {
		case TypeEntry:
			entries = append(entries, pack.ManifestEntry{
				Path: resp.Path,
				Hash: resp.Hash,
				Mode: resp.Mode,
				Size: resp.Size,
			})
		case TypeManifestDone:
			exists := resp.Exists != nil && *resp.Exists
			elapsed := time.Duration(
				resp.ElapsedMs,
			) * time.Millisecond
			return entries, exists, elapsed, nil
		case TypeError:
			if resp.Fatal {
				return nil, false, 0,
					fmt.Errorf("%s", resp.Message)
			}
		default:
			return nil, false, 0, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

type PackResult struct {
	Dest  string
	Size  int64
	Count int
}

func (s *Session) Pack(
	dir string,
	paths []string,
	compress bool,
) (*PackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ext string
	if compress {
		ext = ".tar.gz"
	} else {
		ext = ".tar"
	}
	dest := tmpPath(ext)

	err := s.sendCmd(Request{
		Cmd:      "pack",
		Dir:      dir,
		Paths:    paths,
		Dest:     dest,
		Compress: compress,
	})
	if err != nil {
		return nil, err
	}

	for {
		resp, err := s.readResponse()
		if err != nil {
			return nil, err
		}
		switch resp.Type {
		case TypePackDone:
			return &PackResult{
				Dest:  resp.Dest,
				Size:  resp.Size,
				Count: resp.Count,
			}, nil
		case TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("%s", resp.Message)
			}
		default:
			return nil, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

type ExtractResult struct {
	Count int
}

func (s *Session) Extract(
	dir, src string,
	compress bool,
) (*ExtractResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.sendCmd(Request{
		Cmd:      "extract",
		Dir:      dir,
		Src:      src,
		Compress: compress,
	})
	if err != nil {
		return nil, err
	}

	for {
		resp, err := s.readResponse()
		if err != nil {
			return nil, err
		}
		switch resp.Type {
		case TypeExtractDone:
			return &ExtractResult{Count: resp.Count}, nil
		case TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("%s", resp.Message)
			}
		default:
			return nil, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

type DeleteResult struct {
	Count int
}

func (s *Session) Delete(
	dir string,
	paths []string,
) (*DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.sendCmd(Request{
		Cmd:   "delete",
		Dir:   dir,
		Paths: paths,
	})
	if err != nil {
		return nil, err
	}

	for {
		resp, err := s.readResponse()
		if err != nil {
			return nil, err
		}
		switch resp.Type {
		case TypeDeleteDone:
			return &DeleteResult{Count: resp.Count}, nil
		case TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("%s", resp.Message)
			}
		default:
			return nil, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

type TransferResult struct {
	Count int
	Size  int64
	Dest  string
}

func (s *Session) Transfer(
	dir string,
	paths []string,
	compress bool,
	destURL string,
	token string,
) (*TransferResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.sendCmd(Request{
		Cmd:      "transfer",
		Dir:      dir,
		Paths:    paths,
		Compress: compress,
		URL:      destURL,
		Token:    token,
	})
	if err != nil {
		return nil, err
	}

	for {
		resp, err := s.readResponse()
		if err != nil {
			return nil, err
		}
		switch resp.Type {
		case TypeTransferDone:
			return &TransferResult{
				Count: resp.Count,
				Size:  resp.Size,
				Dest:  resp.Dest,
			}, nil
		case TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("%s", resp.Message)
			}
		default:
			return nil, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sendCmd(Request{Cmd: "quit"})

	done := make(chan struct{})
	go func() {
		<-s.conn.Done()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
	}

	closeErr := s.conn.Close()

	s.client.ExecHTTP(
		ctx, s.sprite,
		[]string{"rm", "-f", s.remoteBin},
		nil,
	)

	return closeErr
}

func tmpPath(ext string) string {
	var b [8]byte
	rand.Read(b[:])
	return fmt.Sprintf("/tmp/sprync-%s%s",
		hex.EncodeToString(b[:]),
		ext,
	)
}
