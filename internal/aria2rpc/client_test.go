package aria2rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientVersionAddsJSONRPCPathAndToken(t *testing.T) {
	var gotPath string
	var gotMethod string
	var gotParams []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotMethod = req.Method
		gotParams = req.Params
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"version":         "1.37.0",
				"enabledFeatures": []string{"BitTorrent", "SFTP"},
			},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, "secret-value")
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/jsonrpc" {
		t.Fatalf("path=%q, want /jsonrpc", gotPath)
	}
	if gotMethod != "aria2.getVersion" {
		t.Fatalf("method=%q", gotMethod)
	}
	if len(gotParams) != 1 || gotParams[0] != "token:secret-value" {
		t.Fatalf("params=%#v", gotParams)
	}
	if v.Version != "1.37.0" || len(v.EnabledFeatures) != 2 {
		t.Fatalf("unexpected version response: %#v", v)
	}
}

func TestClientAddURI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method != "aria2.addUri" {
			t.Fatalf("method=%q", req.Method)
		}
		if len(req.Params) != 2 {
			t.Fatalf("params=%#v", req.Params)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "0123456789abcdef"})
	}))
	defer srv.Close()

	gid, err := New(srv.URL+"/jsonrpc", "").AddURI(context.Background(), []string{"magnet:?xt=urn:btih:test"}, map[string]any{"gid": "0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if gid != "0123456789abcdef" {
		t.Fatalf("gid=%q", gid)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&RPCError{Code: 1, Message: "GID deadbeef is not found"}) {
		t.Fatal("expected not-found RPC error to be recognized")
	}
	if IsNotFound(&RPCError{Code: 1, Message: "permission denied"}) {
		t.Fatal("unexpected not-found classification")
	}
}
