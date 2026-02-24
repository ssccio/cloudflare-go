// Package rayid implements the `cf rayid` subcommand group.
package rayid

import "github.com/spf13/cobra"

// Cmd is the `cf rayid` parent command.
var Cmd = &cobra.Command{
	Use:   "rayid",
	Short: "Investigate Cloudflare Ray IDs",
	Long: `Look up a Cloudflare Ray ID to retrieve the action taken,
firewall rule matched, client metadata, and request details.

Ray IDs are visible in the CF-Ray response header and in Cloudflare
security event logs.`,
}

func init() {
	Cmd.AddCommand(lookupCmd)
}
