package mcp

import "context"

// ToolBackend is implemented by the layer that provides real tool execution.
type ToolBackend interface {
	Retain(ctx context.Context, bankID string, args map[string]any) (any, error)
	Recall(ctx context.Context, bankID string, args map[string]any) (any, error)
	ListBanks(ctx context.Context, bankID string, args map[string]any) (any, error)
	GetBank(ctx context.Context, bankID string, args map[string]any) (any, error)
	Postmortem(ctx context.Context, bankID string, args map[string]any) (any, error)
	AckPostmortem(ctx context.Context, bankID string, args map[string]any) (any, error)
}
