package modeltui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/singularity-ng/singularity-memory/go/internal/modelcatalog"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c Client) Catalog(ctx context.Context) (modelcatalog.Catalog, error) {
	var out modelcatalog.Catalog
	if err := c.doJSON(ctx, http.MethodGet, "/v1/model-catalog/", nil, &out); err != nil {
		return modelcatalog.Catalog{}, err
	}
	return out, nil
}

func (c Client) Sync(ctx context.Context) (modelcatalog.Catalog, error) {
	var out modelcatalog.Catalog
	if err := c.doJSON(ctx, http.MethodPost, "/v1/model-catalog/sync", []byte("{}"), &out); err != nil {
		return modelcatalog.Catalog{}, err
	}
	return out, nil
}

func (c Client) doJSON(ctx context.Context, method, path string, body []byte, out any) error {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:8888"
	}
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s returned %s", method, path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
