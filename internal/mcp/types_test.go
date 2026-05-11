package mcp

import (
	"encoding/json"
	"testing"
)

func TestJSONRPCRequestRoundTrip(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "abc",
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got JSONRPCRequest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.JSONRPC != req.JSONRPC {
		t.Errorf("jsonrpc = %q, want %q", got.JSONRPC, req.JSONRPC)
	}
	if got.Method != req.Method {
		t.Errorf("method = %q, want %q", got.Method, req.Method)
	}
	if got.ID != req.ID {
		t.Errorf("id = %v, want %v", got.ID, req.ID)
	}
}

func TestJSONRPCResponseRoundTrip(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      1,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got JSONRPCResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.JSONRPC != resp.JSONRPC {
		t.Errorf("jsonrpc = %q, want %q", got.JSONRPC, resp.JSONRPC)
	}
	// JSON numbers unmarshal to float64, so compare via JSON round-trip.
	if got.ID == nil {
		t.Errorf("id = nil, want %v", resp.ID)
	} else if gotID, ok := got.ID.(float64); !ok || int(gotID) != resp.ID {
		t.Errorf("id = %v, want %v", got.ID, resp.ID)
	}
	if got.Error != nil {
		t.Errorf("unexpected error: %+v", got.Error)
	}
}

func TestJSONRPCResponseErrorRoundTrip(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      2,
		Error: &JSONRPCError{
			Code:    ErrMethodNotFound,
			Message: "Method not found",
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got JSONRPCResponse
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Error == nil {
		t.Fatal("expected error, got nil")
	}
	if got.Error.Code != ErrMethodNotFound {
		t.Errorf("error code = %d, want %d", got.Error.Code, ErrMethodNotFound)
	}
	if got.Error.Message != "Method not found" {
		t.Errorf("error message = %q, want %q", got.Error.Message, "Method not found")
	}
}

func TestIsNotification(t *testing.T) {
	if !(&JSONRPCRequest{Method: "foo"}).IsNotification() {
		t.Error("expected notification for nil ID")
	}
	if (&JSONRPCRequest{ID: "x", Method: "foo"}).IsNotification() {
		t.Error("expected not notification for string ID")
	}
	if (&JSONRPCRequest{ID: 1, Method: "foo"}).IsNotification() {
		t.Error("expected not notification for int ID")
	}
}

func TestJSONRPCContentBlock(t *testing.T) {
	cb := JSONRPCContentBlock{Type: "text", Text: "hello"}
	b, err := json.Marshal(cb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got JSONRPCContentBlock
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != cb.Type || got.Text != cb.Text {
		t.Errorf("got %+v, want %+v", got, cb)
	}
}
