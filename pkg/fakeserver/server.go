package fakeserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

type Server struct {
	HS      *httptest.Server
	RootDir string
	mu      sync.Mutex
	procs   []*os.Process
}

func New(rootDir string) *Server {
	s := &Server{RootDir: rootDir}
	mux := http.NewServeMux()
	prefix := "/v1/sprites/test-sprite"
	mux.HandleFunc(prefix+"/fs/write", s.handleFSWrite)
	mux.HandleFunc(prefix+"/fs/read", s.handleFSRead)
	mux.HandleFunc(prefix+"/exec", s.handleExec)
	s.HS = httptest.NewServer(mux)
	return s
}

func (s *Server) Close() {
	s.mu.Lock()
	for _, p := range s.procs {
		p.Kill()
	}
	s.mu.Unlock()
	s.HS.Close()
}

func (s *Server) URL() string {
	return s.HS.URL
}

func (s *Server) resolvePath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Join(s.RootDir, p)
	}
	return filepath.Join(s.RootDir, p)
}

func (s *Server) handleFSWrite(
	w http.ResponseWriter, r *http.Request,
) {
	if r.Method != "PUT" {
		http.Error(w, "method not allowed", 405)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path", 400)
		return
	}
	modeStr := r.URL.Query().Get("mode")

	target := s.resolvePath(path)
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0755); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	mode := os.FileMode(0644)
	if modeStr != "" {
		m, err := strconv.ParseUint(modeStr, 8, 32)
		if err == nil {
			mode = os.FileMode(m)
		}
	}

	f, err := os.OpenFile(
		target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	n, _ := io.Copy(f, r.Body)
	f.Close()

	resp := map[string]any{
		"path": path,
		"size": n,
		"mode": fmt.Sprintf("%04o", mode),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleFSRead(
	w http.ResponseWriter, r *http.Request,
) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "missing path", 400)
		return
	}

	target := s.resolvePath(path)
	f, err := os.Open(target)
	if err != nil {
		if filepath.IsAbs(path) {
			f, err = os.Open(path)
		}
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
	}
	defer f.Close()

	w.Header().Set(
		"Content-Type", "application/octet-stream",
	)
	io.Copy(w, f)
}

func (s *Server) handleExec(
	w http.ResponseWriter, r *http.Request,
) {
	if !isWebSocketUpgrade(r) {
		s.handleExecHTTP(w, r)
		return
	}
	s.handleExecWS(w, r)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(
		r.Header.Get("Upgrade"), "websocket",
	) || strings.EqualFold(
		r.Header.Get("Connection"), "upgrade",
	)
}

func (s *Server) handleExecHTTP(
	w http.ResponseWriter, r *http.Request,
) {
	cmdArgs := r.URL.Query()["cmd"]
	if len(cmdArgs) == 0 {
		http.Error(w, "missing cmd", 400)
		return
	}

	resolved := s.resolvePath(cmdArgs[0])
	args := cmdArgs[1:]

	cmd := exec.Command(resolved, args...)
	cmd.Dir = s.RootDir
	if r.Body != nil {
		cmd.Stdin = r.Body
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		w.WriteHeader(500)
	}
	w.Write(out)
}

func (s *Server) handleExecWS(
	w http.ResponseWriter, r *http.Request,
) {
	cmdArgs := r.URL.Query()["cmd"]
	if len(cmdArgs) == 0 {
		http.Error(w, "missing cmd", 400)
		return
	}

	resolved := s.resolvePath(cmdArgs[0])
	args := cmdArgs[1:]

	conn, err := websocket.Accept(
		w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	)
	if err != nil {
		return
	}
	conn.SetReadLimit(16 << 20)

	ctx := r.Context()

	cmd := exec.Command(resolved, args...)
	cmd.Dir = s.RootDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	s.mu.Lock()
	s.procs = append(s.procs, cmd.Process)
	s.mu.Unlock()

	sessionInfo := map[string]any{
		"type":       "session_info",
		"session_id": "fake-session-1",
		"command":    cmdArgs,
		"is_owner":   true,
		"tty":        false,
	}
	infoJSON, _ := json.Marshal(sessionInfo)
	conn.Write(ctx, websocket.MessageText, infoJSON)

	inputCtx, inputCancel := context.WithCancel(ctx)
	defer inputCancel()

	var outWG sync.WaitGroup
	outWG.Add(2)
	go func() {
		defer outWG.Done()
		pumpOutput(ctx, conn, stdout, 0x01)
	}()
	go func() {
		defer outWG.Done()
		pumpOutput(ctx, conn, stderr, 0x02)
	}()

	go pumpInput(inputCtx, conn, stdin)

	outWG.Wait()

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	inputCancel()

	exitMsg := []byte{0x03, byte(exitCode)}
	conn.Write(ctx, websocket.MessageBinary, exitMsg)

	exitJSON, _ := json.Marshal(map[string]any{
		"type":      "exit",
		"exit_code": exitCode,
	})
	conn.Write(ctx, websocket.MessageText, exitJSON)

	conn.Close(websocket.StatusNormalClosure, "")
}

func pumpOutput(
	ctx context.Context,
	conn *websocket.Conn,
	r io.Reader,
	prefix byte,
) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			msg := make([]byte, 1+n)
			msg[0] = prefix
			copy(msg[1:], buf[:n])
			conn.Write(ctx, websocket.MessageBinary, msg)
		}
		if err != nil {
			return
		}
	}
}

func pumpInput(
	ctx context.Context,
	conn *websocket.Conn,
	w io.WriteCloser,
) {
	defer w.Close()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if len(data) == 0 {
			continue
		}
		switch data[0] {
		case 0x00:
			w.Write(data[1:])
		case 0x04:
			return
		}
	}
}

func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}
