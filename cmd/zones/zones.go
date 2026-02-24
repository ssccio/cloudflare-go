// Package zones implements the `cf zones` subcommand group.
package zones

import "github.com/spf13/cobra"

// Cmd is the `cf zones` parent command.
var Cmd = &cobra.Command{
	Use:   "zones",
	Short: "List and look up Cloudflare zones",
	Long:  "List all zones in your Cloudflare account or look up a zone ID by domain name.",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(lookupCmd)
}
