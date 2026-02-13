package spriteapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

func (c *Client) ExecWebSocket(
	ctx context.Context,
	sprite string,
	cmd []string,
	stdin bool,
) (*websocket.Conn, error) {
	q := url.Values{}
	for _, arg := range cmd {
		q.Add("cmd", arg)
	}
	if stdin {
		q.Set("stdin", "true")
	}

	httpURL := fmt.Sprintf("%s?%s",
		c.spriteURL(sprite, "/exec"),
		q.Encode(),
	)
	wsURL := httpToWS(httpURL)

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{
				"Bearer " + c.Token,
			},
		},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(16 << 20)
	return conn, nil
}

func httpToWS(u string) string {
	if rest, ok := strings.CutPrefix(u, "https://"); ok {
		return "wss://" + rest
	}
	if rest, ok := strings.CutPrefix(u, "http://"); ok {
		return "ws://" + rest
	}
	return u
}
