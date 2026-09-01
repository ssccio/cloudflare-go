// Package analytics implements the `cf analytics` subcommand group, querying
// the Cloudflare GraphQL Analytics API for zone traffic, threats, and WAF events.
package analytics

import "github.com/spf13/cobra"

// Cmd is the `cf analytics` parent command.
var Cmd = &cobra.Command{
	Use:   "analytics",
	Short: "Query Cloudflare zone analytics",
	Long:  "Query the Cloudflare GraphQL Analytics API for traffic, threats, and WAF events.",
}

func init() {
	Cmd.AddCommand(trafficCmd)
	Cmd.AddCommand(threatsCmd)
	Cmd.AddCommand(wafEventsCmd)
}
