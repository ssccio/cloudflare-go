// Package zones implements the `cf zones` subcommand group.
package zones

import "github.com/spf13/cobra"

// Cmd is the `cf zones` parent command.
var Cmd = &cobra.Command{
	Use:   "zones",
	Short: "List and inspect Cloudflare zones",
	Long:  "List zones, look up a zone ID by domain name, and inspect a zone's details and settings.",
}

func init() {
	Cmd.AddCommand(infoCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(lookupCmd)
	Cmd.AddCommand(settingsCmd)
}
