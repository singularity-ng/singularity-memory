package mcp

// JSON-RPC 2.0 error codes used by the MCP wire protocol.
const (
	ErrParseError     = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
	ErrServerError    = -32000
)

// JSONRPCRequest is a JSON-RPC 2.0 request object.
type JSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// JSONRPCError is the error object inside a JSON-RPC response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response object.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCContentBlock represents a content block in a tool result (text or image).
type JSONRPCContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// IsNotification returns true if the request has no ID (notification).
func (r *JSONRPCRequest) IsNotification() bool {
	return r.ID == nil
}
