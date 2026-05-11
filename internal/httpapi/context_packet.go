package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

type contextPacketRequest struct {
	Query     string `json:"query,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

type contextPacketResponse struct {
	BankID      string                  `json:"bank_id"`
	Query       string                  `json:"query,omitempty"`
	Mode        string                  `json:"mode"`
	CoreMemory  []store.CoreMemoryBlock `json:"core_memory"`
	Reflection  *store.Reflection       `json:"reflection,omitempty"`
	Sections    []contextPacketSection  `json:"sections"`
	TokenBudget int                     `json:"token_budget,omitempty"`
	Usage       tokenUsage              `json:"usage"`
	Warnings    []string                `json:"warnings,omitempty"`
}

type contextPacketSection struct {
	Name      string `json:"name"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

func (s *server) contextPacket(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	if bankID == "" {
		writeError(w, http.StatusBadRequest, "bank_id is required")
		return
	}

	var req contextPacketRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Mode == "" {
		req.Mode = "agent"
	}

	blocks, err := s.deps.Store.ListCoreMemoryBlocks(r.Context(), bankID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list core memory failed")
		return
	}

	reflection, err := s.deps.Store.ReflectAgentMemory(r.Context(), bankID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reflect memory failed")
		return
	}

	sections := make([]contextPacketSection, 0, len(blocks)+1)
	usedTokens := 0
	for _, block := range blocks {
		text, truncated := truncateForPacket(block.Content, remainingPacketTokens(req.MaxTokens, usedTokens))
		if text == "" {
			continue
		}
		usedTokens += estimateTokens(text)
		sections = append(sections, contextPacketSection{
			Name:      "core_memory:" + block.BlockName,
			Text:      text,
			Truncated: truncated,
		})
	}
	if reflection != nil {
		text, truncated := truncateForPacket(reflectionText(reflection), remainingPacketTokens(req.MaxTokens, usedTokens))
		if text != "" {
			usedTokens += estimateTokens(text)
			sections = append(sections, contextPacketSection{
				Name:      "reflection",
				Text:      text,
				Truncated: truncated,
			})
		}
	}

	warnings := []string(nil)
	if strings.TrimSpace(req.Query) != "" {
		warnings = append(warnings, "query recall is available at /v1/default/banks/{bank_id}/memories/recall; context packet currently assembles core memory and reflection")
	}

	writeJSON(w, http.StatusOK, contextPacketResponse{
		BankID:      bankID,
		Query:       req.Query,
		Mode:        req.Mode,
		CoreMemory:  blocks,
		Reflection:  reflection,
		Sections:    sections,
		TokenBudget: req.MaxTokens,
		Usage: tokenUsage{
			InputTokens:  estimateTokens(req.Query),
			OutputTokens: usedTokens,
			TotalTokens:  estimateTokens(req.Query) + usedTokens,
		},
		Warnings: warnings,
	})
}

func remainingPacketTokens(maxTokens int, usedTokens int) int {
	if maxTokens <= 0 {
		return 0
	}
	return maxTokens - usedTokens
}

func reflectionText(reflection *store.Reflection) string {
	if reflection == nil {
		return ""
	}
	var parts []string
	for _, observation := range reflection.Observations {
		if strings.TrimSpace(observation.Text) != "" {
			parts = append(parts, observation.Text)
		}
	}
	for _, page := range reflection.Pages {
		if strings.TrimSpace(page.Content) != "" {
			parts = append(parts, page.Content)
		}
	}
	return strings.Join(parts, "\n")
}

func truncateForPacket(text string, remainingTokens int) (string, bool) {
	if remainingTokens == 0 {
		return text, false
	}
	if remainingTokens < 0 {
		return "", true
	}
	return truncateWords(text, remainingTokens)
}
