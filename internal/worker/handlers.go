package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/llm"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/modelrouter"
	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/store"
)

// handleSleep runs the Letta-style reflection loop for a bank.
func handleSleep(ctx context.Context, bankID string, job *store.BrainJob, s Store, route modelrouter.Route, logger *log.Logger, sharedBankID string) error {
	reflection, err := s.ReflectAgentMemory(ctx, bankID, 200)
	if err != nil {
		return fmt.Errorf("reflect: %w", err)
	}

	if len(reflection.Observations) == 0 && len(reflection.Pages) == 0 {
		if _, err := s.RunDeterministicConsolidation(ctx, bankID, 50); err != nil {
			logger.Warn("sleep: deterministic consolidation failed", "bank_id", bankID, "error", err)
		}
		return nil
	}

	var sb strings.Builder
	for _, obs := range reflection.Observations {
		sb.WriteString("- ")
		sb.WriteString(obs.Text)
		sb.WriteString("\n")
	}
	for _, page := range reflection.Pages {
		sb.WriteString("- [page] ")
		sb.WriteString(page.Title)
		sb.WriteString(": ")
		sb.WriteString(page.Content)
		sb.WriteString("\n")
	}
	memoriesText := strings.TrimSpace(sb.String())

	userMsg := fmt.Sprintf(`Here are recent memories from ops agent bank %q:

%s

Extract and return a JSON object with these fields:
{
  "observations": ["high-level insight 1", "..."],
  "core_updates": {
    "active_incidents": "updated summary or null",
    "runbook_index": "updated one-liner index or null"
  }
}

Rules:
- observations: 3-8 high-signal patterns worth remembering long-term. Prefer: root causes identified, commands that worked, runbook gaps, service failure patterns.
- core_updates: only include if you see clear updates needed. null means no change.
- Return only valid JSON. No markdown, no explanation.`, bankID, memoriesText)

	client := llm.New(route)
	raw, err := client.Complete(ctx,
		[]llm.Message{{Role: "user", Content: userMsg}},
		llm.WithSystemPrompt("You are an ops memory consolidator. Your job is to synthesize raw operational memories into durable observations."),
		llm.WithMaxTokens(2048),
		llm.WithTemperature(0.2),
	)
	if err != nil {
		return fmt.Errorf("llm complete: %w", err)
	}

	var parsed struct {
		Observations []string       `json:"observations"`
		CoreUpdates  map[string]any `json:"core_updates"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &parsed); err != nil {
		return fmt.Errorf("parse llm response: %w: raw=%q", err, raw)
	}

	now := time.Now().UTC()
	for _, obs := range parsed.Observations {
		obs = strings.TrimSpace(obs)
		if obs == "" {
			continue
		}
		if _, err := s.InsertMemoryUnit(ctx, bankID, &store.MemoryUnit{
			BankID:    bankID,
			Text:      obs,
			FactType:  "observation",
			EventDate: now,
			Tags:      []string{"sleep-worker"},
		}); err != nil {
			logger.Warn("sleep: insert observation failed", "bank_id", bankID, "error", err)
		}
	}

	for blockName, rawVal := range parsed.CoreUpdates {
		if rawVal == nil {
			continue
		}
		content, ok := rawVal.(string)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		if _, err := s.UpsertCoreMemoryBlock(ctx, store.CoreMemoryBlock{
			BankID:    bankID,
			BlockName: blockName,
			Content:   content,
		}); err != nil {
			logger.Warn("sleep: upsert core block failed", "bank_id", bankID, "block", blockName, "error", err)
		}
	}

	if _, err := s.RunDeterministicConsolidation(ctx, bankID, 50); err != nil {
		logger.Warn("sleep: deterministic consolidation failed", "bank_id", bankID, "error", err)
	}

	// Cross-agent propagation: write high-confidence observations to the shared bank
	// so all agents benefit from patterns discovered in any agent's bank.
	if sharedBankID != "" && sharedBankID != bankID {
		for _, obs := range parsed.Observations {
			obs = strings.TrimSpace(obs)
			if obs == "" {
				continue
			}
			// Only propagate clearly high-signal observations
			// (heuristic: observations mentioning root causes, patterns, or runbook gaps)
			lc := strings.ToLower(obs)
			isHighSignal := strings.Contains(lc, "root cause") ||
				strings.Contains(lc, "pattern") ||
				strings.Contains(lc, "runbook") ||
				strings.Contains(lc, "next time") ||
				strings.Contains(lc, "always check") ||
				strings.Contains(lc, "typically") ||
				strings.Contains(lc, "outage")
			if !isHighSignal {
				continue
			}
			if _, err := s.InsertMemoryUnit(ctx, sharedBankID, &store.MemoryUnit{
				BankID:    sharedBankID,
				Text:      fmt.Sprintf("[from %s] %s", bankID, obs),
				FactType:  "experience",
				EventDate: now,
				Tags:      []string{"cross-agent", "sleep-worker", "source:" + bankID},
			}); err != nil {
				logger.Warn("sleep: cross-agent propagation failed", "shared_bank", sharedBankID, "source_bank", bankID, "error", err)
			}
		}
		logger.Info("sleep: cross-agent propagation done", "shared_bank", sharedBankID, "source_bank", bankID)
	}

	return nil
}

// handleHindsight extracts post-incident lessons and stores them as memory units.
func handleHindsight(ctx context.Context, bankID string, job *store.BrainJob, s Store, route modelrouter.Route, logger *log.Logger) error {
	fp, _ := job.Params["fingerprint"].(string)
	rootCause, _ := job.Params["root_cause"].(string)
	resolution, _ := job.Params["resolution"].(string)
	resolvedBy, _ := job.Params["resolved_by"].(string)

	fpTag := "fingerprint:" + fp

	rows, err := s.Query(ctx,
		`SELECT text FROM memory_units WHERE bank_id = $1 AND $2 = ANY(tags) ORDER BY created_at DESC LIMIT 50`,
		bankID, fpTag,
	)
	if err != nil {
		return fmt.Errorf("query fingerprint memories: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			continue
		}
		lines = append(lines, "- "+content)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan fingerprint memories: %w", err)
	}

	if len(lines) == 0 {
		logger.Info("hindsight: no memories for fingerprint, skipping", "bank_id", bankID, "fingerprint", fp)
		return nil
	}

	timeline := strings.Join(lines, "\n")

	userMsg := fmt.Sprintf(`Incident resolved. Here is what happened:

Fingerprint: %s
Root cause: %s
Resolution: %s
Resolved by: %s

Agent activity during incident:
%s

With hindsight, extract lessons as a JSON object:
{
  "lessons": ["next time X fires, check Y first", "..."],
  "runbook_gaps": ["runbook step Z was wrong: ...", "..."],
  "wrong_predictions": ["agent predicted X but actual cause was Y"]
}

Rules:
- lessons: 2-5 actionable "next time" insights for ops agents
- runbook_gaps: specific runbook deficiencies discovered. Empty array if none.
- wrong_predictions: what was misdiagnosed. Empty array if none.
- Return only valid JSON.`, fp, rootCause, resolution, resolvedBy, timeline)

	client := llm.New(route)
	raw, err := client.Complete(ctx,
		[]llm.Message{{Role: "user", Content: userMsg}},
		llm.WithSystemPrompt("You are an ops hindsight analyst. Your job is to extract durable lessons from resolved incidents."),
		llm.WithMaxTokens(2048),
		llm.WithTemperature(0.2),
	)
	if err != nil {
		return fmt.Errorf("llm complete: %w", err)
	}

	var parsed struct {
		Lessons           []string `json:"lessons"`
		RunbookGaps       []string `json:"runbook_gaps"`
		WrongPredictions  []string `json:"wrong_predictions"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &parsed); err != nil {
		return fmt.Errorf("parse llm response: %w: raw=%q", err, raw)
	}

	now := time.Now().UTC()
	for _, lesson := range parsed.Lessons {
		lesson = strings.TrimSpace(lesson)
		if lesson == "" {
			continue
		}
		if _, err := s.InsertMemoryUnit(ctx, bankID, &store.MemoryUnit{
			BankID:    bankID,
			Text:      lesson,
			FactType:  "experience",
			EventDate: now,
			Tags:      []string{"hindsight", fpTag},
		}); err != nil {
			logger.Warn("hindsight: insert lesson failed", "bank_id", bankID, "error", err)
		}
	}

	for _, gap := range parsed.RunbookGaps {
		gap = strings.TrimSpace(gap)
		if gap == "" {
			continue
		}
		if _, err := s.InsertMemoryUnit(ctx, bankID, &store.MemoryUnit{
			BankID:    bankID,
			Text:      gap,
			FactType:  "observation",
			EventDate: now,
			Tags:      []string{"runbook-gap", fpTag},
		}); err != nil {
			logger.Warn("hindsight: insert runbook gap failed", "bank_id", bankID, "error", err)
		}
	}

	return nil
}

// extractJSON strips any markdown code fences and returns the first JSON object found.
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	// Strip ```json ... ``` fences
	if idx := strings.Index(raw, "```"); idx != -1 {
		raw = raw[idx:]
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		if end := strings.LastIndex(raw, "```"); end != -1 {
			raw = raw[:end]
		}
		raw = strings.TrimSpace(raw)
	}
	// Find first { to last }
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start != -1 && end != -1 && end > start {
		return raw[start : end+1]
	}
	return raw
}
