// Package client wraps the Cloudflare v6 SDK client construction and
// configuration loading from environment variables and CLI flags.
package client

import (
	"fmt"
	"os"

	"github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
)

// Config holds auth configuration resolved from flags and environment.
type Config struct {
	Token string
}

// New builds a Cloudflare API client from config.
// Precedence: --token flag > CLOUDFLARE_API_TOKEN env var.
func New(cfg Config) (*cloudflare.Client, error) {
	token := cfg.Token
	if token == "" {
		token = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf(
			"Cloudflare API token required: set CLOUDFLARE_API_TOKEN or use --token",
		)
	}
	return cloudflare.NewClient(option.WithAPIToken(token)), nil
}
