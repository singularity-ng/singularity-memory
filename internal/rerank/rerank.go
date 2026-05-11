package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/log"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/config"
)

// Client calls an OpenAI-compatible gateway /v1/rerank endpoint.
type Client struct {
	cfg    config.Config
	client *http.Client
	logger *log.Logger
}

// NewClient creates a rerank client from config.
func NewClient(cfg config.Config, logger *log.Logger) *Client {
	return &Client{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

// Result is a single reranked document with its relevance score.
type Result struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// rerankRequest mirrors the Cohere rerank request shape.
type rerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      int      `json:"top_n"`
}

// rerankResponse mirrors the Cohere rerank response shape.
type rerankResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

// Rerank sends documents to the rerank gateway and returns scored results
// mapped back to the original document order.
func (c *Client) Rerank(ctx context.Context, query string, documents []string) ([]Result, error) {
	if len(documents) == 0 {
		return nil, nil
	}

	start := time.Now()

	topN := c.cfg.RerankTopK
	if topN <= 0 {
		topN = 10
	}

	reqBody := rerankRequest{
		Model:     c.cfg.RerankModel,
		Query:     query,
		Documents: documents,
		TopN:      topN,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	url := endpointURL(c.cfg.RerankGatewayURL, "rerank")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.RerankAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.RerankAPIKey)
	}

	c.logger.Info("rerank request",
		"url", url,
		"model", c.cfg.RerankModel,
		"document_count", len(documents),
		"top_n", topN,
	)

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("rerank request failed", "error", err, "url", url, "model", c.cfg.RerankModel)
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rerank response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Error("rerank non-OK response",
			"status", resp.StatusCode,
			"url", url,
			"model", c.cfg.RerankModel,
			"latency_ms", latency.Milliseconds(),
		)
		return nil, fmt.Errorf("rerank gateway returned %d (%s) for model %s", resp.StatusCode, http.StatusText(resp.StatusCode), c.cfg.RerankModel)
	}

	var parsed rerankResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal rerank response: %w", err)
	}

	// Map results back to original document order by index.
	sort.Slice(parsed.Results, func(i, j int) bool {
		return parsed.Results[i].Index < parsed.Results[j].Index
	})

	results := make([]Result, len(parsed.Results))
	for i, r := range parsed.Results {
		results[i] = Result{
			Index:          r.Index,
			RelevanceScore: r.RelevanceScore,
		}
	}

	c.logger.Info("rerank response",
		"model", c.cfg.RerankModel,
		"document_count", len(documents),
		"result_count", len(results),
		"latency_ms", latency.Milliseconds(),
	)

	return results, nil
}

func endpointURL(baseURL, operation string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + operation
	}
	return base + "/v1/" + operation
}
