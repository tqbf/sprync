package protocol

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
)

const (
	PrefixStdin    byte = 0x00
	PrefixStdout   byte = 0x01
	PrefixStderr   byte = 0x02
	PrefixExit     byte = 0x03
	PrefixStdinEOF byte = 0x04
)

type WSConn struct {
	ws       *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	stdout   *io.PipeReader
	stderr   *io.PipeReader
	stdoutW  *io.PipeWriter
	stderrW  *io.PipeWriter
	exitCh   chan struct{}
	exitCode int
	session  string
	mu       sync.Mutex
}

func NewWSConn(
	ctx context.Context,
	ws *websocket.Conn,
) *WSConn {
	ctx, cancel := context.WithCancel(ctx)
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	c := &WSConn{
		ws:      ws,
		ctx:     ctx,
		cancel:  cancel,
		stdout:  stdoutR,
		stderr:  stderrR,
		stdoutW: stdoutW,
		stderrW: stderrW,
		exitCh:  make(chan struct{}),
	}

	go c.readPump()
	return c
}

func (c *WSConn) Stdout() io.Reader { return c.stdout }
func (c *WSConn) Stderr() io.Reader { return c.stderr }

func (c *WSConn) ExitCode() int {
	<-c.exitCh
	return c.exitCode
}

func (c *WSConn) Done() <-chan struct{} {
	return c.exitCh
}

func (c *WSConn) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *WSConn) WriteStdin(data []byte) error {
	msg := make([]byte, 1+len(data))
	msg[0] = PrefixStdin
	copy(msg[1:], data)
	return c.ws.Write(
		c.ctx, websocket.MessageBinary, msg,
	)
}

func (c *WSConn) CloseStdin() error {
	return c.ws.Write(
		c.ctx, websocket.MessageBinary,
		[]byte{PrefixStdinEOF},
	)
}

func (c *WSConn) Close() error {
	c.cancel()
	err := c.ws.Close(
		websocket.StatusNormalClosure, "",
	)
	if err != nil {
		select {
		case <-c.exitCh:
			return nil
		default:
		}
	}
	return err
}

func (c *WSConn) readPump() {
	defer c.stdoutW.Close()
	defer c.stderrW.Close()

	for {
		typ, data, err := c.ws.Read(c.ctx)
		if err != nil {
			select {
			case <-c.exitCh:
			default:
				c.stdoutW.CloseWithError(err)
				c.stderrW.CloseWithError(err)
			}
			return
		}

		if typ == websocket.MessageText {
			c.handleTextFrame(data)
			continue
		}

		if len(data) == 0 {
			continue
		}

		switch data[0] {
		case PrefixStdout:
			c.stdoutW.Write(data[1:])
		case PrefixStderr:
			c.stderrW.Write(data[1:])
		case PrefixExit:
			if len(data) > 1 {
				c.exitCode = int(data[1])
			}
			close(c.exitCh)
			return
		}
	}
}

func (c *WSConn) handleTextFrame(data []byte) {
	var msg struct {
		Type      string `json:"type"`
		SessionID string `json:"session_id"`
		ExitCode  int    `json:"exit_code"`
	}
	if json.Unmarshal(data, &msg) != nil {
		return
	}

	switch msg.Type {
	case "session_info":
		c.mu.Lock()
		c.session = msg.SessionID
		c.mu.Unlock()
	case "exit":
		c.exitCode = msg.ExitCode
		select {
		case <-c.exitCh:
		default:
			close(c.exitCh)
		}
	default:
		slog.Debug("unknown ws text frame",
			"type", msg.Type,
			"data", string(data),
		)
	}
}
