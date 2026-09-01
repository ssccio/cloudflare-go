// Package health implements the `cf health` subcommand group.
package health

import "github.com/spf13/cobra"

// Cmd is the `cf health` parent command.
var Cmd = &cobra.Command{
	Use:   "health",
	Short: "Manage Cloudflare health checks",
	Long:  "Create, list, and delete health checks within a Cloudflare zone.",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(deleteCmd)
}
