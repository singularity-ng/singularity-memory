package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestServerProbe(t *testing.T) {
	srv := NewServer()
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty body, got %v", body)
	}
}

func TestServerInitialize(t *testing.T) {
	srv := NewServer()
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-1")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}

	sess := srv.Sessions.Get("sess-1")
	if sess == nil {
		t.Fatal("expected session")
	}
	if !sess.Initialized {
		t.Error("expected session initialized")
	}
}

func TestServerToolsList(t *testing.T) {
	srv := NewServer()
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/list",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-2")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected result map, got %T", resp.Result)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("expected tools array, got %T", result["tools"])
	}
	if len(tools) == 0 {
		t.Error("expected at least one tool")
	}
}

func TestServerToolsCallRetain(t *testing.T) {
	srv := NewServer()
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "memory_retain",
			"arguments": map[string]any{
				"content": "hello world",
				"context": "test",
			},
		},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-3")
	req.Header.Set("X-Bank-Id", "default")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestServerAuthRejectsMissingToken(t *testing.T) {
	os.Setenv("SINGULARITY_MCP_AUTH_TOKEN", "secret123")
	defer os.Unsetenv("SINGULARITY_MCP_AUTH_TOKEN")

	srv := NewServer()
	srv.AuthToken = "secret123"
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      4,
		Method:  "initialize",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-4")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestServerAuthAcceptsValidToken(t *testing.T) {
	os.Setenv("SINGULARITY_MCP_AUTH_TOKEN", "secret123")
	defer os.Unsetenv("SINGULARITY_MCP_AUTH_TOKEN")

	srv := NewServer()
	srv.AuthToken = "secret123"
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      5,
		Method:  "initialize",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-5")
	req.Header.Set("Authorization", "Bearer secret123")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestServerMethodNotFound(t *testing.T) {
	srv := NewServer()
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      6,
		Method:  "unknown/method",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-6")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp JSONRPCResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, ErrMethodNotFound)
	}
}

func TestServerNotification(t *testing.T) {
	srv := NewServer()
	payload := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  "initialized/notification",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-7")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
