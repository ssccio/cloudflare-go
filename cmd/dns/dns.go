// Package dns implements the `cf dns` subcommand group.
package dns

import "github.com/spf13/cobra"

// Cmd is the `cf dns` parent command.
var Cmd = &cobra.Command{
	Use:   "dns",
	Short: "Manage Cloudflare DNS records",
	Long:  "Create, list, and manage DNS records within a Cloudflare zone.",
}

func init() {
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(deleteCmd)
}
