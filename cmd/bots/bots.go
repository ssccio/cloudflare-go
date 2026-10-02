// Package bots implements the `cf bots` subcommand group: zone Bot Management
// configuration and the bot score / fingerprint picture of a zone's traffic.
package bots

import "github.com/spf13/cobra"

// Cmd is the `cf bots` parent command.
var Cmd = &cobra.Command{
	Use:   "bots",
	Short: "Inspect Cloudflare Bot Management",
	Long:  "Read a zone's Bot Management configuration and break its traffic down by bot score and JA4 fingerprint.",
}

func init() {
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(scoresCmd)
}
