// Package tunnels implements the `cf tunnels` subcommand group.
package tunnels

import "github.com/spf13/cobra"

// Cmd is the `cf tunnels` parent command.
var Cmd = &cobra.Command{
	Use:   "tunnels",
	Short: "Manage Cloudflare Tunnels",
	Long:  "Create remotely managed Cloudflare Tunnels (cloudflared).",
}

func init() {
	Cmd.AddCommand(createCmd)
}
