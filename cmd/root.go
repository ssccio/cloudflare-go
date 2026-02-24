// Package cmd is the root of the cfgo CLI command tree.
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/cmd/dns"
	"github.com/ssccio/cloudflare-go/cmd/rayid"
	"github.com/ssccio/cloudflare-go/cmd/zones"
)

// GlobalFlags holds persistent flags shared by all subcommands.
type GlobalFlags struct {
	Token   string
	JSON    bool
	TOON    bool
	NoColor bool
	Quiet   bool
	Query   string
}

// Flags is the single instance of GlobalFlags injected into subcommands.
var Flags GlobalFlags

var rootCmd = &cobra.Command{
	Use:   "cf",
	Short: "cf — Cloudflare CLI",
	Long: `cf is a Cloudflare command-line tool for DNS management,
Ray ID investigation, and more.

Authentication:
  Set CLOUDFLARE_API_TOKEN in your environment, or pass --token.

Output modes:
  Default  Beautiful tables and colored output for human operators.
  --json   Structured JSON to stdout — ideal for AI assistants and scripts.
  --toon   Token-Oriented Object Notation — 30-60% fewer tokens than JSON, ideal for LLMs.
  --query  JMESPath filter on --json/--toon output (e.g. --query '[].id').
  --quiet  Suppress progress/info lines; emit only the result.`,
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&Flags.Token, "token", "",
		"Cloudflare API token (overrides CLOUDFLARE_API_TOKEN)")
	rootCmd.PersistentFlags().BoolVar(&Flags.JSON, "json", false,
		"Machine-readable JSON output — for AI assistant operators")
	rootCmd.PersistentFlags().BoolVar(&Flags.TOON, "toon", false,
		"Token-Oriented Object Notation output — 30-60% fewer tokens than JSON, ideal for LLMs")
	rootCmd.PersistentFlags().BoolVar(&Flags.NoColor, "no-color", false,
		"Disable ANSI color output")
	rootCmd.PersistentFlags().BoolVarP(&Flags.Quiet, "quiet", "q", false,
		"Suppress progress and informational output")
	rootCmd.PersistentFlags().StringVar(&Flags.Query, "query", "",
		"JMESPath expression to filter --json or --toon output (e.g. '[].id')")

	rootCmd.AddCommand(dns.Cmd)
	rootCmd.AddCommand(rayid.Cmd)
	rootCmd.AddCommand(zones.Cmd)
}
