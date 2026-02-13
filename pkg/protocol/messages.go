package protocol

import (
	"encoding/json"
	"fmt"
)

type Request struct {
	Cmd      string   `json:"cmd"`
	Dir      string   `json:"dir,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	Dest     string   `json:"dest,omitempty"`
	Src      string   `json:"src,omitempty"`
	Compress bool     `json:"compress,omitempty"`
}

type ResponseType string

const (
	TypeReady        ResponseType = "ready"
	TypeEntry        ResponseType = "entry"
	TypeManifestDone ResponseType = "manifest_done"
	TypePackDone     ResponseType = "pack_done"
	TypeExtractDone  ResponseType = "extract_done"
	TypeDeleteDone   ResponseType = "delete_done"
	TypeError        ResponseType = "error"
)

type Response struct {
	Type    ResponseType `json:"type"`
	Version string       `json:"version,omitempty"`
	PID     int          `json:"pid,omitempty"`

	Path string `json:"path,omitempty"`
	Hash string `json:"hash,omitempty"`
	Mode int    `json:"mode,omitempty"`
	Size int64  `json:"size,omitempty"`

	Count     int   `json:"count,omitempty"`
	Exists    *bool `json:"exists,omitempty"`
	ElapsedMs int64 `json:"elapsed_ms,omitempty"`

	Dest string `json:"dest,omitempty"`

	Message string `json:"message,omitempty"`
	Fatal   bool   `json:"fatal,omitempty"`
}

func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	if req.Cmd == "" {
		return nil, fmt.Errorf("missing cmd field")
	}
	return &req, nil
}

func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if resp.Type == "" {
		return nil, fmt.Errorf("missing type field")
	}
	return &resp, nil
}

func BoolPtr(b bool) *bool {
	return &b
}
