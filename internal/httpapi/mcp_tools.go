package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/mcp"
)

// mcpToolBackend implements mcp.ToolBackend by delegating to the real HTTP handlers.
type mcpToolBackend struct {
	s *server
}

// ensure mcpToolBackend implements mcp.ToolBackend
var _ mcp.ToolBackend = (*mcpToolBackend)(nil)

func newMCPToolBackend(s *server) *mcpToolBackend {
	return &mcpToolBackend{s: s}
}

// Retain delegates to the retain handler.
func (b *mcpToolBackend) Retain(ctx context.Context, bankID string, args map[string]any) (any, error) {
	item := retainItem{
		Content:    getString(args, "content"),
		Context:    getString(args, "context"),
		DocumentID: getString(args, "document_id"),
	}
	if tags, ok := args["tags"].([]any); ok {
		item.Tags = toStringSlice(tags)
	}
	if md, ok := args["metadata"].(map[string]any); ok {
		item.Metadata = md
	}

	reqBody, _ := json.Marshal(retainRequest{Items: []retainItem{item}})
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/"+bankID+"/memories", bytes.NewReader(reqBody))
	req = req.WithContext(ctx)
	req = withBankID(req, bankID)
	rec := httptest.NewRecorder()
	b.s.retain(rec, req)

	return parseHandlerResponse(rec)
}

// Recall delegates to the recall handler.
func (b *mcpToolBackend) Recall(ctx context.Context, bankID string, args map[string]any) (any, error) {
	reqBody := recallRequest{
		Query:     getString(args, "query"),
		Budget:    getString(args, "budget"),
		MaxTokens: getInt(args, "max_tokens"),
		TagsMatch: getString(args, "tags_match"),
	}
	if typesArg, ok := args["types"].([]any); ok {
		reqBody.Types = toStringSlice(typesArg)
	}
	if tagsArg, ok := args["tags"].([]any); ok {
		reqBody.Tags = toStringSlice(tagsArg)
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/"+bankID+"/memories/recall", bytes.NewReader(body))
	req = req.WithContext(ctx)
	req = withBankID(req, bankID)
	rec := httptest.NewRecorder()
	b.s.recall(rec, req)

	return parseHandlerResponse(rec)
}

// ListBanks delegates to the listBanks handler.
func (b *mcpToolBackend) ListBanks(ctx context.Context, bankID string, args map[string]any) (any, error) {
	req := httptest.NewRequest(http.MethodGet, "/v1/banks", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	b.s.listBanks(rec, req)

	return parseHandlerResponse(rec)
}

// GetBank delegates to the getBank handler.
func (b *mcpToolBackend) GetBank(ctx context.Context, bankID string, args map[string]any) (any, error) {
	targetBank := bankID
	if bArg, ok := args["bank_id"].(string); ok && bArg != "" {
		targetBank = bArg
	}
	if targetBank == "" {
		return nil, fmt.Errorf("no bank_id configured")
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/default/banks/"+targetBank+"/profile", nil)
	req = req.WithContext(ctx)
	req = withBankID(req, targetBank)
	rec := httptest.NewRecorder()
	b.s.getBank(rec, req)

	return parseHandlerResponse(rec)
}

// Postmortem triggers post-incident analysis for a resolved alert fingerprint.
func (b *mcpToolBackend) Postmortem(ctx context.Context, bankID string, args map[string]any) (any, error) {
	reqBody := postmortemRequest{
		Fingerprint: getString(args, "fingerprint"),
	}
	if slugs, ok := args["runbook_slugs"].([]any); ok {
		reqBody.RunbookSlugs = toStringSlice(slugs)
	}
	if reqBody.Fingerprint == "" {
		return nil, fmt.Errorf("fingerprint is required")
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/default/banks/"+bankID+"/postmortem", bytes.NewReader(body))
	req = req.WithContext(ctx)
	req = withBankID(req, bankID)
	rec := httptest.NewRecorder()
	b.s.enqueuePostmortem(rec, req)

	return parseHandlerResponse(rec)
}

// parseHandlerResponse reads an httptest.ResponseRecorder and returns the parsed JSON body or an error.
func parseHandlerResponse(rec *httptest.ResponseRecorder) (any, error) {
	if rec.Code >= 400 {
		var errBody map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
		msg := "handler error"
		if m, ok := errBody["error"].(string); ok {
			msg = m
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var result any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case int64:
		return int(v)
	}
	return 0
}

func toStringSlice(v []any) []string {
	out := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func withBankID(req *http.Request, bankID string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("bank_id", bankID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
