package speech

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

const (
	generationURL      = "https://openrouter.ai/api/v1/generation"
	generationAttempts = 10
	generationBackoff  = 750 * time.Millisecond
)

// GenerationCost asks OpenRouter what a finished generation cost, in USD.
// OpenRouter prices a generation a few seconds after serving it, so the
// lookup keeps retrying for a while before giving up.
func (c *Client) GenerationCost(ctx context.Context, id string) (float64, error) {
	var lastErr error

	for attempt := range generationAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(generationBackoff):
			}
		}

		cost, err := c.fetchGenerationCost(ctx, id)
		if err == nil {
			return cost, nil
		}

		lastErr = err
	}

	return 0, lastErr
}

func (c *Client) fetchGenerationCost(ctx context.Context, id string) (float64, error) {
	resp, err := c.do(ctx, "GET", generationURL+"?id="+url.QueryEscape(id), nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var out struct {
		Data struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("decoding generation: %w", err)
	}

	return out.Data.TotalCost, nil
}
