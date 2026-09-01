// Package cmdutil holds the boilerplate every subcommand repeats: reading the
// root's persistent flags, building a Printer, and resolving a zone.
package cmdutil

import (
	"fmt"
	"os"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

// Context carries everything a subcommand needs after flag parsing.
type Context struct {
	Printer *output.Printer
	Client  *cf.Client
	Token   string
	ZoneID  string
}

// Setup reads the root persistent flags and returns a Printer plus the token.
// The token falls back to CLOUDFLARE_API_TOKEN here, not just inside the SDK
// client, because callers that hit the GraphQL API build their own requests.
func Setup(cmd *cobra.Command) (*output.Printer, string) {
	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	toonFlag, _ := cmd.Root().PersistentFlags().GetBool("toon")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")
	query, _ := cmd.Root().PersistentFlags().GetString("query")

	if token == "" {
		token = os.Getenv("CLOUDFLARE_API_TOKEN")
	}

	return output.New(jsonFlag, toonFlag, quiet, noColor, query), token
}

// Zone does the full Setup plus client construction and zone resolution.
// zone may be a zone ID or a zone name; domain is an explicit zone name.
// At least one must be non-empty.
func Zone(cmd *cobra.Command, zone, domain string) (*Context, error) {
	p, token := Setup(cmd)

	if zone == "" && domain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return nil, err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return nil, err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, zone, domain)
	if err != nil {
		p.Error("%v", err)
		return nil, err
	}

	return &Context{Printer: p, Client: cfClient, Token: token, ZoneID: zoneID}, nil
}

// DryRun reports a would-do action and returns true when dryRun is set, so
// callers can `if cmdutil.DryRun(p, dryRun, "create %s", name) { return nil }`.
func DryRun(p *output.Printer, dryRun bool, format string, args ...any) bool {
	if !dryRun {
		return false
	}
	p.Notice("[DRY RUN] Would %s", fmt.Sprintf(format, args...))
	return true
}
