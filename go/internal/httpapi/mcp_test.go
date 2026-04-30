package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/mcp"
)

func TestMCPRouteProbe(t *testing.T) {
	mcpSrv := mcp.NewServer()
	handler := NewServer(Dependencies{
		Config:    config.Config{DatabaseSchema: "public", MCPEnabled: true},
		MCPServer: mcpSrv,
	})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMCPRouteInitialize(t *testing.T) {
	mcpSrv := mcp.NewServer()
	handler := NewServer(Dependencies{
		Config:    config.Config{DatabaseSchema: "public", MCPEnabled: true},
		MCPServer: mcpSrv,
	})

	payload := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params:  map[string]any{},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
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
}

func TestMCPRouteToolsCallWithHeaderBank(t *testing.T) {
	mcpSrv := mcp.NewServer()
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			MCPEnabled:        true,
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{insertMemoryUnitID: "unit-123"},
		EmbedClient: &fakeEmbedClient{},
		MCPServer:   mcpSrv,
	})

	payload := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "memory_retain",
			"arguments": map[string]any{
				"content": "hello world",
			},
		},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-2")
	req.Header.Set("X-Bank-Id", "mybank")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	sess := mcpSrv.Sessions.Get("sess-2")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.BankID != "mybank" {
		t.Errorf("bankID = %q, want mybank", sess.BankID)
	}
}

func TestMCPRouteToolsCallWithPathBank(t *testing.T) {
	mcpSrv := mcp.NewServer()
	handler := NewServer(Dependencies{
		Config: config.Config{
			DatabaseSchema:    "public",
			MCPEnabled:        true,
			RetainBatchTokens: 8000,
		},
		Store:       fakeStore{insertMemoryUnitID: "unit-123"},
		EmbedClient: &fakeEmbedClient{},
		MCPServer:   mcpSrv,
	})

	payload := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      3,
		Method:  "tools/call",
		Params: map[string]any{
			"name": "memory_retain",
			"arguments": map[string]any{
				"content": "hello world",
			},
		},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp/pathbank", bytes.NewReader(b))
	req.Header.Set("Mcp-Session-Id", "sess-3")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	sess := mcpSrv.Sessions.Get("sess-3")
	if sess == nil {
		t.Fatal("expected session")
	}
	if sess.BankID != "pathbank" {
		t.Errorf("bankID = %q, want pathbank", sess.BankID)
	}
}

func TestMCPRouteDisabled(t *testing.T) {
	handler := NewServer(Dependencies{
		Config:    config.Config{DatabaseSchema: "public", MCPEnabled: false},
		MCPServer: nil,
	})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when MCP disabled, got %d", rec.Code)
	}
}

func TestMCPRouteSlashVariants(t *testing.T) {
	mcpSrv := mcp.NewServer()
	handler := NewServer(Dependencies{
		Config:    config.Config{DatabaseSchema: "public", MCPEnabled: true},
		MCPServer: mcpSrv,
	})

	for _, path := range []string{"/mcp", "/mcp/", "/mcp/default", "/mcp/default/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", path, rec.Code)
		}
	}
}
