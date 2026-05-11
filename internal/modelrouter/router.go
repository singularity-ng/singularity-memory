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
	TaskPostmortem       Task = "postmortem"
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

// taskEnvPrefix maps each task to its env var prefix for per-task overrides.
// Full route: OPS_MEMORY_WORKER_<PREFIX>_BASE_URL / _API_KEY / _MODEL
// Model-only: OPS_MEMORY_WORKER_MODEL_<PREFIX> (legacy, still supported)
var taskEnvPrefix = map[Task]string{
	TaskSummarizeTurn:   "SUMMARIZE_TURN",
	TaskConsolidateBank: "CONSOLIDATE_BANK",
	TaskPostmortem:       "POSTMORTEM",
	TaskExtractEntities: "EXTRACT_ENTITIES",
	TaskImportanceScore: "IMPORTANCE_SCORE",
}

// New builds a Router from env vars, reading at construction time.
//
// Default route resolution order:
//  1. OPS_MEMORY_WORKER_LLM_BASE_URL + OPS_MEMORY_WORKER_LLM_API_KEY + OPS_MEMORY_WORKER_MODEL
//  2. First cfg.ModelDiscoveryEndpoints entry with a BaseURL
//  3. Nothing → configured=false → Route() returns false, LLM jobs are skipped
//
// Per-task full overrides: OPS_MEMORY_WORKER_<TASK>_BASE_URL / _API_KEY / _MODEL
// Per-task model-only:     OPS_MEMORY_WORKER_MODEL_<TASK> (legacy)
func New(cfg config.Config) *Router {
	workerBaseURL := os.Getenv("OPS_MEMORY_WORKER_LLM_BASE_URL")
	workerAPIKey := os.Getenv("OPS_MEMORY_WORKER_LLM_API_KEY")
	workerModel := firstNonEmpty(os.Getenv("OPS_MEMORY_WORKER_MODEL"), cfg.WorkerLLMModel)

	var baseURL, apiKey, defaultModel string

	switch {
	case workerBaseURL != "":
		baseURL = workerBaseURL
		apiKey = workerAPIKey
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

	taskRoutes := make(map[Task]Route, len(taskEnvPrefix))
	for task, prefix := range taskEnvPrefix {
		taskBaseURL := os.Getenv("OPS_MEMORY_WORKER_" + prefix + "_BASE_URL")
		taskAPIKey := os.Getenv("OPS_MEMORY_WORKER_" + prefix + "_API_KEY")
		taskModel := firstNonEmpty(
			os.Getenv("OPS_MEMORY_WORKER_"+prefix+"_MODEL"),
			os.Getenv("OPS_MEMORY_WORKER_MODEL_"+prefix), // legacy
		)
		switch {
		case taskBaseURL != "":
			// Full provider override for this task.
			taskRoutes[task] = Route{
				BaseURL: taskBaseURL,
				APIKey:  firstNonEmpty(taskAPIKey, apiKey),
				Model:   firstNonEmpty(taskModel, defaultModel),
			}
		case taskModel != "":
			// Model-only override — inherit base URL + key from default.
			taskRoutes[task] = Route{BaseURL: baseURL, Model: taskModel, APIKey: apiKey}
		}
	}

	return &Router{
		defaultRoute: defaultRoute,
		taskRoutes:   taskRoutes,
		configured:   baseURL != "" || hasAnyTaskRoute(taskRoutes),
	}
}

// Route returns the best Route for the given task.
// Returns ({}, false) if no LLM is configured for this task.
func (r *Router) Route(_ context.Context, task Task) (Route, bool) {
	if route, ok := r.taskRoutes[task]; ok && route.BaseURL != "" {
		return route, true
	}
	if r.defaultRoute.BaseURL != "" {
		return r.defaultRoute, true
	}
	return Route{}, false
}

func hasAnyTaskRoute(routes map[Task]Route) bool {
	for _, r := range routes {
		if r.BaseURL != "" {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
