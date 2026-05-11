package modelrouter

import (
	"context"
	"os"
	"strings"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
)

type Task string

const (
	TaskSummarizeTurn   Task = "summarize_turn"
	TaskConsolidateBank Task = "consolidate_bank"
	TaskHindsight       Task = "hindsight"
	TaskExtractEntities Task = "extract_entities"
	TaskImportanceScore Task = "importance_score"
)

// Route is an OpenAI-compatible LLM target.
type Route struct {
	BaseURL string
	Model   string
	APIKey  string
}

// Router holds pre-resolved routes for each task class.
type Router struct {
	defaultRoute Route
	taskRoutes   map[Task]Route
	configured   bool
}

// New builds a Router from env vars, reading at construction time.
//
// Base URL / API key / model resolution order:
//  1. OPS_MEMORY_WORKER_LLM_BASE_URL + OPS_MEMORY_WORKER_LLM_API_KEY + OPS_MEMORY_WORKER_MODEL
//  2. cfg.EmbedGatewayURL + cfg.EmbedAPIKey + OPS_MEMORY_WORKER_MODEL (shared LLM gateway)
//  3. First cfg.ModelDiscoveryEndpoints entry with a BaseURL; model from endpoint.Model else OPS_MEMORY_WORKER_MODEL
//  4. Nothing → configured=false → Route() returns false, LLM jobs are skipped
//
// Per-task model overrides: OPS_MEMORY_WORKER_MODEL_<TASK_UPPER>
func New(cfg config.Config) *Router {
	workerBaseURL := firstNonEmpty(os.Getenv("OPS_MEMORY_WORKER_LLM_BASE_URL"))
	workerAPIKey := firstNonEmpty(os.Getenv("OPS_MEMORY_WORKER_LLM_API_KEY"))
	workerModel := firstNonEmpty(os.Getenv("OPS_MEMORY_WORKER_MODEL"), cfg.WorkerLLMModel)

	var baseURL, apiKey, defaultModel string

	switch {
	case workerBaseURL != "":
		baseURL = workerBaseURL
		apiKey = workerAPIKey
		defaultModel = workerModel
	case cfg.EmbedGatewayURL != "":
		baseURL = cfg.EmbedGatewayURL
		apiKey = firstNonEmpty(cfg.EmbedAPIKey, os.Getenv("LLM_MUX_API_KEY"))
		defaultModel = workerModel
	default:
		for _, ep := range cfg.ModelDiscoveryEndpoints {
			if ep.BaseURL != "" {
				baseURL = ep.BaseURL
				apiKey = ep.APIKey
				defaultModel = firstNonEmpty(ep.Model, workerModel)
				break
			}
		}
	}

	defaultRoute := Route{BaseURL: baseURL, Model: defaultModel, APIKey: apiKey}

	taskEnvs := map[Task]string{
		TaskSummarizeTurn:   "OPS_MEMORY_WORKER_MODEL_SUMMARIZE_TURN",
		TaskConsolidateBank: "OPS_MEMORY_WORKER_MODEL_CONSOLIDATE_BANK",
		TaskHindsight:       "OPS_MEMORY_WORKER_MODEL_HINDSIGHT",
		TaskExtractEntities: "OPS_MEMORY_WORKER_MODEL_EXTRACT_ENTITIES",
		TaskImportanceScore: "OPS_MEMORY_WORKER_MODEL_IMPORTANCE_SCORE",
	}
	taskRoutes := make(map[Task]Route, len(taskEnvs))
	for task, envKey := range taskEnvs {
		if model := os.Getenv(envKey); model != "" {
			taskRoutes[task] = Route{BaseURL: baseURL, Model: model, APIKey: apiKey}
		}
	}

	return &Router{
		defaultRoute: defaultRoute,
		taskRoutes:   taskRoutes,
		configured:   baseURL != "",
	}
}

// Route returns the best Route for the given task.
// Returns ({}, false) if no LLM base URL is configured.
func (r *Router) Route(_ context.Context, task Task) (Route, bool) {
	if !r.configured {
		return Route{}, false
	}
	if route, ok := r.taskRoutes[task]; ok {
		return route, true
	}
	return r.defaultRoute, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
