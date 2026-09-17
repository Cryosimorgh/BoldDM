package aria2rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Client struct {
	endpoint string
	secret   string
	http     *http.Client
	seq      atomic.Uint64
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	if e == nil {
		return "aria2 RPC error"
	}
	return fmt.Sprintf("aria2 RPC %d: %s", e.Code, e.Message)
}

type Version struct {
	Version         string   `json:"version"`
	EnabledFeatures []string `json:"enabledFeatures"`
}

type File struct {
	Index           string `json:"index"`
	Path            string `json:"path"`
	Length          string `json:"length"`
	CompletedLength string `json:"completedLength"`
	Selected        string `json:"selected"`
}

type Status struct {
	GID             string   `json:"gid"`
	Status          string   `json:"status"`
	TotalLength     string   `json:"totalLength"`
	CompletedLength string   `json:"completedLength"`
	UploadLength    string   `json:"uploadLength"`
	DownloadSpeed   string   `json:"downloadSpeed"`
	UploadSpeed     string   `json:"uploadSpeed"`
	Connections     string   `json:"connections"`
	ErrorCode       string   `json:"errorCode"`
	ErrorMessage    string   `json:"errorMessage"`
	Dir             string   `json:"dir"`
	FollowedBy      []string `json:"followedBy"`
	Files           []File   `json:"files"`
	BitTorrent      struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
	} `json:"bittorrent"`
}

func New(endpoint, secret string) *Client {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:6800/jsonrpc"
	} else if !strings.HasSuffix(strings.ToLower(endpoint), "/jsonrpc") {
		endpoint += "/jsonrpc"
	}
	return &Client{
		endpoint: endpoint,
		secret:   secret,
		http:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Endpoint() string { return c.endpoint }

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	if c.secret != "" {
		params = append([]any{"token:" + c.secret}, params...)
	}
	id := strconv.FormatUint(c.seq.Add(1), 10)
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aria2 RPC request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("aria2 RPC HTTP %s", resp.Status)
	}
	var decoded rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return fmt.Errorf("decode aria2 RPC response: %w", err)
	}
	if decoded.Error != nil {
		return decoded.Error
	}
	if out == nil || len(decoded.Result) == 0 || string(decoded.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, out); err != nil {
		return fmt.Errorf("decode aria2 RPC result: %w", err)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var out Version
	err := c.call(ctx, "aria2.getVersion", nil, &out)
	return out, err
}

func (c *Client) AddURI(ctx context.Context, uris []string, options map[string]any) (string, error) {
	var gid string
	err := c.call(ctx, "aria2.addUri", []any{uris, options}, &gid)
	return gid, err
}

func (c *Client) TellStatus(ctx context.Context, gid string) (Status, error) {
	keys := []string{"gid", "status", "totalLength", "completedLength", "uploadLength", "downloadSpeed", "uploadSpeed", "connections", "errorCode", "errorMessage", "dir", "followedBy", "files", "bittorrent"}
	var out Status
	err := c.call(ctx, "aria2.tellStatus", []any{gid, keys}, &out)
	return out, err
}

func (c *Client) ChangeOption(ctx context.Context, gid string, options map[string]any) error {
	var ignored string
	return c.call(ctx, "aria2.changeOption", []any{gid, options}, &ignored)
}

func (c *Client) Pause(ctx context.Context, gid string) error {
	var ignored string
	return c.call(ctx, "aria2.forcePause", []any{gid}, &ignored)
}

func (c *Client) Unpause(ctx context.Context, gid string) error {
	var ignored string
	return c.call(ctx, "aria2.unpause", []any{gid}, &ignored)
}

func (c *Client) Remove(ctx context.Context, gid string) error {
	status, err := c.TellStatus(ctx, gid)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	if status.Status == "active" || status.Status == "waiting" || status.Status == "paused" {
		var ignored string
		if err := c.call(ctx, "aria2.forceRemove", []any{gid}, &ignored); err != nil && !IsNotFound(err) {
			return err
		}
	}
	var ignored string
	if err := c.call(ctx, "aria2.removeDownloadResult", []any{gid}, &ignored); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

func (c *Client) Shutdown(ctx context.Context) error {
	var ignored string
	return c.call(ctx, "aria2.shutdown", nil, &ignored)
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		m := strings.ToLower(rpcErr.Message)
		return strings.Contains(m, "not found") || strings.Contains(m, "cannot find")
	}
	return false
}
