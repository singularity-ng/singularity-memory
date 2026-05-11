package mcp

import (
	"fmt"
	"net/http"
)

// ToolSchema describes an MCP tool for tools/list.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// handleToolsList returns the list of available tools.
func (s *Server) handleToolsList(req *JSONRPCRequest) *JSONRPCResponse {
	tools := []ToolSchema{
		{
			Name:        "memory_retain",
			Description: "Store information to long-term memory. Use this tool to save facts, preferences, events, or any data you want to remember later. Be specific and include relevant details.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The fact/memory to store (be specific and include relevant details)",
					},
					"context": map[string]any{
						"type":        "string",
						"description": "Category for the memory (e.g., 'preferences', 'work', 'hobbies', 'family'). Default: 'general'",
						"default":     "general",
					},
					"timestamp": map[string]any{
						"type":        "string",
						"description": "When this event/fact occurred (ISO format, e.g., '2024-01-15T10:30:00Z'). Useful for timeline tracking.",
					},
					"tags": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional tags for scoped visibility filtering (e.g., ['project:alpha', 'user:123'])",
					},
					"metadata": map[string]any{
						"type":        "object",
						"description": "Optional key-value metadata to attach (e.g., {'source': 'slack', 'channel': 'general'})",
					},
					"document_id": map[string]any{
						"type":        "string",
						"description": "Optional document ID to associate this memory with",
					},
					"strategy": map[string]any{
						"type":        "string",
						"description": "Optional named retain strategy (e.g., 'exact' for verbatim storage). Strategies are defined in the bank config.",
					},
					"update_mode": map[string]any{
						"type":        "string",
						"description": "How to handle existing documents with the same document_id. 'replace' (default) or 'append' (concatenates new content to existing).",
					},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:        "memory_recall",
			Description: "Search long-term memory for relevant information. Use this tool when you need to recall facts, preferences, or past events about the user or context.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language search query (e.g., \"user's food preferences\", \"what projects is user working on\")",
					},
					"max_tokens": map[string]any{
						"type":        "integer",
						"description": "Maximum tokens to return in results (default: 4096)",
						"default":     4096,
					},
					"budget": map[string]any{
						"type":        "string",
						"description": "Search budget - 'low', 'mid', or 'high' (default: 'high'). Higher budgets search more thoroughly.",
						"default":     "high",
					},
					"types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Fact types to include (e.g., ['world', 'experience']). Default: all types.",
					},
					"tags": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Optional tags to filter results by (e.g., ['project:alpha'])",
					},
					"tags_match": map[string]any{
						"type":        "string",
						"description": "How to match tags - 'any' (match any tag) or 'all' (match all tags). Default: 'any'",
						"default":     "any",
					},
					"query_timestamp": map[string]any{
						"type":        "string",
						"description": "Temporal context for the query (ISO format, e.g., '2024-01-15T10:30:00Z'). Helps retrieve time-relevant memories.",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "memory_list_banks",
			Description: "List all available memory banks. Use this tool to discover what memory banks exist in the system. Each bank is an isolated memory store (like a separate \"brain\").",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "memory_get_bank",
			Description: "Get the profile of a memory bank. Returns bank metadata including name, disposition, and mission.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bank_id": map[string]any{
						"type":        "string",
						"description": "Optional bank (defaults to session bank). Use for cross-bank operations.",
					},
				},
			},
		},
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"tools": tools,
		},
	}
}

// handleToolsCall dispatches tool invocations.
func (s *Server) handleToolsCall(r *http.Request, req *JSONRPCRequest, sess *Session) *JSONRPCResponse {
	params, ok := req.Params["arguments"].(map[string]any)
	if !ok {
		// Some clients send args directly in params.
		params = req.Params
	}

	toolName, _ := req.Params["name"].(string)
	if toolName == "" {
		// Fallback: try to extract from params directly.
		toolName, _ = params["name"].(string)
	}

	bankID := ""
	if sess != nil {
		bankID = sess.BankID
	}

	var result any
	var err error

	ctx := r.Context()

	switch toolName {
	case "memory_retain":
		if s.ToolBackend == nil {
			return errorResponse(req.ID, ErrServerError, "tool backend not configured")
		}
		result, err = s.ToolBackend.Retain(ctx, bankID, params)
	case "memory_recall":
		if s.ToolBackend == nil {
			return errorResponse(req.ID, ErrServerError, "tool backend not configured")
		}
		result, err = s.ToolBackend.Recall(ctx, bankID, params)
	case "memory_list_banks":
		if s.ToolBackend == nil {
			return errorResponse(req.ID, ErrServerError, "tool backend not configured")
		}
		result, err = s.ToolBackend.ListBanks(ctx, bankID, params)
	case "memory_get_bank":
		if s.ToolBackend == nil {
			return errorResponse(req.ID, ErrServerError, "tool backend not configured")
		}
		result, err = s.ToolBackend.GetBank(ctx, bankID, params)
	default:
		return errorResponse(req.ID, ErrMethodNotFound, "tool not found: "+toolName)
	}

	if err != nil {
		return errorResponse(req.ID, ErrInternalError, err.Error())
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []JSONRPCContentBlock{
				{Type: "text", Text: fmt.Sprintf("%v", result)},
			},
		},
	}
}
