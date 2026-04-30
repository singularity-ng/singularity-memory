package mcp

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// Server implements the MCP JSON-RPC 2.0 HTTP transport.
type Server struct {
	Sessions  *SessionStore
	AuthToken string
	BankID    string // default bank from header
}

// NewServer creates an MCP server with an in-memory session store.
func NewServer() *Server {
	return &Server{
		Sessions:  NewSessionStore(),
		AuthToken: os.Getenv("SINGULARITY_MCP_AUTH_TOKEN"),
	}
}

// ServeHTTP handles JSON-RPC requests over HTTP.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// GET without session ID is a probe.
	if r.Method == http.MethodGet {
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
	}

	// Auth check.
	if s.AuthToken != "" {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.AuthToken {
			writeJSONError(w, http.StatusUnauthorized, ErrInvalidRequest, "unauthorized")
			return
		}
	}

	// Session handling.
	sessionID := r.Header.Get("Mcp-Session-Id")
	var sess *Session
	if sessionID != "" {
		sess = s.Sessions.GetOrCreate(sessionID)
		sess.BankID = r.Header.Get("X-Bank-Id")
		if sess.BankID == "" {
			sess.BankID = s.BankID
		}
	}

	// Parse JSON-RPC request.
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, ErrParseError, "parse error: "+err.Error())
		return
	}

	resp := s.handleRequest(r, &req, sess)
	if resp == nil {
		// Notification — no response.
		w.WriteHeader(http.StatusAccepted)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleRequest(r *http.Request, req *JSONRPCRequest, sess *Session) *JSONRPCResponse {
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, ErrInvalidRequest, "invalid jsonrpc version")
	}

	// Notifications have no ID — return nil to signal no response body.
	if req.IsNotification() {
		return nil
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req, sess)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(r, req, sess)
	default:
		return errorResponse(req.ID, ErrMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *Server) handleInitialize(req *JSONRPCRequest, sess *Session) *JSONRPCResponse {
	if sess != nil {
		sess.Initialized = true
	}
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "singularity-memory",
				"version": "0.1.0",
			},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code int, message string) {
	writeJSON(w, status, JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	})
}

func errorResponse(id any, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}
}
