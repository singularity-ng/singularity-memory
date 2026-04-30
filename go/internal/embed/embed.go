package embed

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

	"github.com/singularity-ng/singularity-memory/go/internal/config"
)

// Client calls an OpenAI-compatible /v1/embeddings endpoint.
type Client struct {
	cfg    config.Config
	client *http.Client
	logger *log.Logger
}

// NewClient creates an embedding client from config.
func NewClient(cfg config.Config, logger *log.Logger) *Client {
	return &Client{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		logger: logger,
	}
}

// Embedding is a single vector result.
type Embedding struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// embedRequest mirrors the OpenAI embeddings request shape.
type embedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

// embedResponse mirrors the OpenAI embeddings response shape.
type embedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
}

// Embed turns a slice of strings into a slice of float32 vectors.
// It preserves input order and supports batch splitting.
func (c *Client) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	batchSize := c.cfg.EmbedBatchSize
	if batchSize <= 0 {
		batchSize = 32
	}

	var all [][]float32
	for i := 0; i < len(inputs); i += batchSize {
		end := i + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]

		vectors, err := c.embedBatch(ctx, batch, i)
		if err != nil {
			return nil, err
		}
		all = append(all, vectors...)
	}
	return all, nil
}

func (c *Client) embedBatch(ctx context.Context, inputs []string, baseIndex int) ([][]float32, error) {
	start := time.Now()

	reqBody := embedRequest{
		Model: c.cfg.EmbedModel,
		Input: inputs,
	}
	if c.cfg.EmbedDimensions > 0 {
		reqBody.Dimensions = &c.cfg.EmbedDimensions
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := endpointURL(c.cfg.EmbedGatewayURL, "embeddings")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.EmbedAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.EmbedAPIKey)
	}

	c.logger.Info("embed request",
		"url", url,
		"model", c.cfg.EmbedModel,
		"batch_size", len(inputs),
		"dimensions", c.cfg.EmbedDimensions,
	)

	resp, err := c.client.Do(req)
	if err != nil {
		c.logger.Error("embed request failed", "error", err, "url", url, "model", c.cfg.EmbedModel)
		return nil, fmt.Errorf("embed request failed: %w", err)
	}
	defer resp.Body.Close()

	latency := time.Since(start)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Error("embed non-OK response",
			"status", resp.StatusCode,
			"url", url,
			"model", c.cfg.EmbedModel,
			"latency_ms", latency.Milliseconds(),
		)
		return nil, fmt.Errorf("embed gateway returned %d (%s) for model %s", resp.StatusCode, http.StatusText(resp.StatusCode), c.cfg.EmbedModel)
	}

	var parsed embedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embed vector count mismatch: expected %d, got %d (model %s)", len(inputs), len(parsed.Data), c.cfg.EmbedModel)
	}

	// Sort by index to guarantee order preservation.
	sort.Slice(parsed.Data, func(i, j int) bool {
		return parsed.Data[i].Index < parsed.Data[j].Index
	})

	vectors := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		vectors[i] = d.Embedding
	}

	c.logger.Info("embed response",
		"model", parsed.Model,
		"batch_size", len(inputs),
		"dimensions", len(vectors[0]),
		"latency_ms", latency.Milliseconds(),
	)

	return vectors, nil
}

func endpointURL(baseURL, operation string) string {
	base := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/" + operation
	}
	return base + "/v1/" + operation
}
