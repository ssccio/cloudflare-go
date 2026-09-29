// Package cache implements the `cf cache` subcommand group.
package cache

import "github.com/spf13/cobra"

// Cmd is the `cf cache` parent command.
var Cmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the Cloudflare cache",
	Long:  "Purge cached content from a Cloudflare zone.",
}

func init() {
	Cmd.AddCommand(purgeCmd)
}
