package spriteapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		Token:      token,
		HTTPClient: http.DefaultClient,
	}
}

func (c *Client) spriteURL(
	sprite, path string,
) string {
	return fmt.Sprintf(
		"%s/%s%s", c.BaseURL, sprite, path,
	)
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set(
		"Authorization", "Bearer "+c.Token,
	)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return resp, nil
}

type apiError struct {
	StatusCode int
	Message    string
	Code       string
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf(
			"api %d (%s): %s",
			e.StatusCode, e.Code, e.Message,
		)
	}
	return fmt.Sprintf("api %d: %s", e.StatusCode, e.Message)
}

func parseAPIError(status int, body []byte) error {
	var parsed struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != "" {
		return &apiError{
			StatusCode: status,
			Message:    parsed.Error,
			Code:       parsed.Code,
		}
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &apiError{StatusCode: status, Message: msg}
}

func (c *Client) FSWrite(
	ctx context.Context,
	sprite, path string,
	mode string,
	mkdir bool,
	body io.Reader,
) error {
	q := url.Values{}
	q.Set("path", path)
	if mode != "" {
		q.Set("mode", mode)
	}
	if !mkdir {
		q.Set("mkdir", "false")
	}

	u := fmt.Sprintf("%s?%s",
		c.spriteURL(sprite, "/fs/write"),
		q.Encode(),
	)
	req, err := http.NewRequestWithContext(
		ctx, "PUT", u, body,
	)
	if err != nil {
		return err
	}
	req.Header.Set(
		"Content-Type", "application/octet-stream",
	)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) FSRead(
	ctx context.Context,
	sprite, path string,
) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("path", path)

	u := fmt.Sprintf("%s?%s",
		c.spriteURL(sprite, "/fs/read"),
		q.Encode(),
	)
	req, err := http.NewRequestWithContext(
		ctx, "GET", u, nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

type SpriteInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func (c *Client) GetSprite(
	ctx context.Context,
	sprite string,
) (*SpriteInfo, error) {
	u := c.spriteURL(sprite, "")
	req, err := http.NewRequestWithContext(
		ctx, "GET", u, nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info SpriteInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ExecHTTP(
	ctx context.Context,
	sprite string,
	cmd []string,
	stdin io.Reader,
) ([]byte, error) {
	q := url.Values{}
	for _, c := range cmd {
		q.Add("cmd", c)
	}

	u := fmt.Sprintf("%s?%s",
		c.spriteURL(sprite, "/exec"),
		q.Encode(),
	)
	req, err := http.NewRequestWithContext(
		ctx, "POST", u, stdin,
	)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
