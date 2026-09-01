// Package ssl implements the `cf ssl` subcommand group.
package ssl

import "github.com/spf13/cobra"

// Cmd is the `cf ssl` parent command.
var Cmd = &cobra.Command{
	Use:   "ssl",
	Short: "Manage Cloudflare SSL/TLS settings",
	Long:  "View and manage SSL/TLS mode, minimum TLS version, and certificate packs for a Cloudflare zone.",
}

func init() {
	Cmd.AddCommand(statusCmd)
	Cmd.AddCommand(modeCmd)
	Cmd.AddCommand(certificatePacksCmd)
}
