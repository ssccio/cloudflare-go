// Package waf implements the `cf waf` subcommand group.
package waf

import "github.com/spf13/cobra"

// Cmd is the `cf waf` parent command.
var Cmd = &cobra.Command{
	Use:   "waf",
	Short: "Manage Cloudflare WAF rulesets and rules",
	Long: `Inspect and modify the WAF configuration of a Cloudflare zone: list
rulesets, list the rules inside a ruleset, add, change or remove individual
rules, and read or change the zone's security level.

Rule mutations use the per-rule endpoints, so a create, update or delete never rewrites
the whole ruleset and cannot clobber a concurrent edit.`,
}

func init() {
	Cmd.AddCommand(listRulesetsCmd)
	Cmd.AddCommand(listRulesCmd)
	Cmd.AddCommand(createRuleCmd)
	Cmd.AddCommand(updateRuleCmd)
	Cmd.AddCommand(deleteRuleCmd)
	Cmd.AddCommand(securityLevelCmd)
}
