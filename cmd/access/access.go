// Package access implements the `cf access` subcommand group.
package access

import "github.com/spf13/cobra"

// Cmd is the `cf access` parent command.
var Cmd = &cobra.Command{
	Use:   "access",
	Short: "Manage Cloudflare Zero Trust Access",
	Long:  "Create Zero Trust Access applications and their policies.",
}

var appsCmd = &cobra.Command{
	Use:   "apps",
	Short: "Manage Access applications",
}

func init() {
	appsCmd.AddCommand(appsCreateCmd)
	Cmd.AddCommand(appsCmd)
}
