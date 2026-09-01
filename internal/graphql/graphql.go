// Package graphql executes parameterized queries against the Cloudflare
// GraphQL Analytics API, which the REST SDK does not cover.
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Endpoint is the Cloudflare GraphQL Analytics API.
const Endpoint = "https://api.cloudflare.com/client/v4/graphql"

// Error is a single GraphQL-level error returned in the response body.
type Error struct {
	Message string `json:"message"`
}

// Do executes a query and unmarshals the response into out. Variables are sent
// as GraphQL variables, never interpolated into the query text.
func Do(ctx context.Context, token, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("marshalling query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return json.Unmarshal(respBody, out)
}

// CheckErrors turns a GraphQL errors array into a Go error. The analytics API
// reports rate-limit exhaustion here with HTTP 200, so callers that skip this
// see a nil result instead of the real reason.
func CheckErrors(errs []Error) error {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}

// Window returns a since/until pair in RFC3339, ending now.
func Window(hours int) (string, string) {
	until := time.Now().UTC()
	since := until.Add(-time.Duration(hours) * time.Hour)
	return since.Format(time.RFC3339), until.Format(time.RFC3339)
}
