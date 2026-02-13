package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/tqbf/sprync/pkg/pack"
	"github.com/tqbf/sprync/pkg/protocol"
)

type Session struct {
	cmd     *exec.Cmd
	enc     *json.Encoder
	scanner *bufio.Scanner
	mu      sync.Mutex
	Version string
	PID     int
}

func Start(spryncdPath string) (*Session, error) {
	cmd := exec.Command(spryncdPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	go func() {
		s := bufio.NewScanner(stderrPipe)
		for s.Scan() {
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 16<<20), 16<<20)

	s := &Session{
		cmd:     cmd,
		enc:     json.NewEncoder(stdin),
		scanner: scanner,
	}

	resp, err := s.readResponse()
	if err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("read ready: %w", err)
	}
	if resp.Type != protocol.TypeReady {
		cmd.Process.Kill()
		return nil, fmt.Errorf(
			"expected ready, got %s", resp.Type,
		)
	}
	s.Version = resp.Version
	s.PID = resp.PID
	return s, nil
}

func (s *Session) send(req protocol.Request) error {
	return s.enc.Encode(req)
}

func (s *Session) readResponse() (*protocol.Response, error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected EOF")
	}
	return protocol.ParseResponse(s.scanner.Bytes())
}

func (s *Session) Manifest(
	dir string,
	excludes []string,
) ([]pack.ManifestEntry, bool, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.send(protocol.Request{
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
		case protocol.TypeEntry:
			entries = append(entries, pack.ManifestEntry{
				Path: resp.Path,
				Hash: resp.Hash,
				Mode: resp.Mode,
				Size: resp.Size,
			})
		case protocol.TypeManifestDone:
			exists := resp.Exists != nil && *resp.Exists
			elapsed := time.Duration(
				resp.ElapsedMs,
			) * time.Millisecond
			return entries, exists, elapsed, nil
		case protocol.TypeError:
			if resp.Fatal {
				return nil, false, 0,
					fmt.Errorf("remote: %s", resp.Message)
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
	pathList []string,
	dest string,
	compress bool,
) (*PackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.send(protocol.Request{
		Cmd:      "pack",
		Dir:      dir,
		Paths:    pathList,
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
		case protocol.TypePackDone:
			return &PackResult{
				Dest:  resp.Dest,
				Size:  resp.Size,
				Count: resp.Count,
			}, nil
		case protocol.TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("remote: %s", resp.Message)
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

	err := s.send(protocol.Request{
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
		case protocol.TypeExtractDone:
			return &ExtractResult{Count: resp.Count}, nil
		case protocol.TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("remote: %s", resp.Message)
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
	pathList []string,
) (*DeleteResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.send(protocol.Request{
		Cmd:   "delete",
		Dir:   dir,
		Paths: pathList,
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
		case protocol.TypeDeleteDone:
			return &DeleteResult{Count: resp.Count}, nil
		case protocol.TypeError:
			if resp.Fatal {
				return nil,
					fmt.Errorf("remote: %s", resp.Message)
			}
		default:
			return nil, fmt.Errorf(
				"unexpected response: %s", resp.Type,
			)
		}
	}
}

func (s *Session) Quit() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.send(protocol.Request{Cmd: "quit"})
	return s.cmd.Wait()
}
