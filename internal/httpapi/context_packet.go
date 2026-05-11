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

	// Order blocks by mode priority.
	blocks = orderBlocksForMode(blocks, req.Mode)

	sections := make([]contextPacketSection, 0, len(blocks)+2)
	usedTokens := 0

	// Core memory blocks — ordered by mode.
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

	// Reflection content — filtered/ordered by mode.
	if reflection != nil {
		reflText, truncated := truncateForPacket(reflectionTextForMode(reflection, req.Mode), remainingPacketTokens(req.MaxTokens, usedTokens))
		if reflText != "" {
			usedTokens += estimateTokens(reflText)
			sections = append(sections, contextPacketSection{
				Name:      "reflection",
				Text:      reflText,
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

// orderBlocksForMode reorders core memory blocks so mode-relevant blocks appear first.
// active_incident: active_incidents → runbook_index → rest
// runbook_lookup:  runbook_index → active_incidents → rest
// agent (default): original order
func orderBlocksForMode(blocks []store.CoreMemoryBlock, mode string) []store.CoreMemoryBlock {
	priority := map[string]int{}
	switch mode {
	case "active_incident":
		priority["active_incidents"] = 0
		priority["runbook_index"] = 1
		priority["persona"] = 2
	case "runbook_lookup":
		priority["runbook_index"] = 0
		priority["active_incidents"] = 2
		priority["persona"] = 1
	default:
		return blocks
	}

	out := make([]store.CoreMemoryBlock, 0, len(blocks))
	rest := make([]store.CoreMemoryBlock, 0)
	scored := make([]struct {
		block store.CoreMemoryBlock
		score int
	}, 0, len(blocks))

	for _, b := range blocks {
		if p, ok := priority[b.BlockName]; ok {
			scored = append(scored, struct {
				block store.CoreMemoryBlock
				score int
			}{b, p})
		} else {
			rest = append(rest, b)
		}
	}
	// Simple insertion sort by score (small N).
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && scored[j].score < scored[j-1].score; j-- {
			scored[j], scored[j-1] = scored[j-1], scored[j]
		}
	}
	for _, s := range scored {
		out = append(out, s.block)
	}
	out = append(out, rest...)
	return out
}

func remainingPacketTokens(maxTokens int, usedTokens int) int {
	if maxTokens <= 0 {
		return 0
	}
	return maxTokens - usedTokens
}

// reflectionTextForMode returns reflection content ordered/filtered for the given mode.
// runbook_lookup: brain pages first (runbook content), then observations.
// active_incident: observations first (recent activity), then pages.
// agent: original order (observations + pages).
func reflectionTextForMode(reflection *store.Reflection, mode string) string {
	if reflection == nil {
		return ""
	}
	var obsParts, pageParts []string
	for _, observation := range reflection.Observations {
		if strings.TrimSpace(observation.Text) != "" {
			obsParts = append(obsParts, observation.Text)
		}
	}
	for _, page := range reflection.Pages {
		if strings.TrimSpace(page.Content) != "" {
			pageParts = append(pageParts, page.Content)
		}
	}
	switch mode {
	case "runbook_lookup":
		// Pages (runbooks/brain pages) before observations.
		return strings.Join(append(pageParts, obsParts...), "\n")
	case "active_incident":
		// Recent observations (incident activity) before pages.
		return strings.Join(append(obsParts, pageParts...), "\n")
	default:
		return strings.Join(append(obsParts, pageParts...), "\n")
	}
}

// reflectionText returns all reflection content in default order (kept for compatibility).
func reflectionText(reflection *store.Reflection) string {
	return reflectionTextForMode(reflection, "agent")
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
