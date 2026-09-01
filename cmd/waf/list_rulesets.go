package waf

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	listRulesetsZone   string
	listRulesetsDomain string
)

// rulesetResult is the serialisable summary of one ruleset.
type rulesetResult struct {
	ID          string `json:"id"                    toon:"id"`
	Name        string `json:"name"                  toon:"name"`
	Phase       string `json:"phase"                 toon:"phase"`
	Kind        string `json:"kind"                  toon:"kind"`
	Version     string `json:"version,omitempty"     toon:"version,omitempty"`
	Description string `json:"description,omitempty" toon:"description,omitempty"`
	LastUpdated string `json:"last_updated,omitempty" toon:"last_updated,omitempty"`
}

var listRulesetsCmd = &cobra.Command{
	Use:   "list-rulesets",
	Short: "List the rulesets in a zone",
	Long: `List every ruleset attached to a Cloudflare zone, with its phase, kind and ID.

The ruleset ID is what the other waf subcommands take as --ruleset-id.

Examples:
  cf waf list-rulesets --zone ZONE_ID
  cf waf list-rulesets --domain example.com
  cf waf list-rulesets --domain example.com --json
  cf waf list-rulesets --domain example.com --json --query "[?phase=='http_request_firewall_custom'].id"`,
	RunE: runListRulesets,
}

func init() {
	listRulesetsCmd.Flags().StringVar(&listRulesetsZone, "zone", "", "Zone ID")
	listRulesetsCmd.Flags().StringVar(&listRulesetsDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	listRulesetsCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runListRulesets(cmd *cobra.Command, _ []string) error {
	cx, err := cmdutil.Zone(cmd, listRulesetsZone, listRulesetsDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	p.Info("Fetching rulesets for zone %s…", cx.ZoneID)

	var out []rulesetResult
	iter := cx.Client.Rulesets.ListAutoPaging(cmd.Context(), rulesets.RulesetListParams{
		ZoneID: cf.F(cx.ZoneID),
	})
	for iter.Next() {
		r := iter.Current()
		out = append(out, rulesetResult{
			ID:          r.ID,
			Name:        r.Name,
			Phase:       string(r.Phase),
			Kind:        string(r.Kind),
			Version:     r.Version,
			Description: r.Description,
			LastUpdated: r.LastUpdated.String(),
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(out)
		return nil
	}

	if len(out) == 0 {
		p.Info("No rulesets found.")
		return nil
	}

	rows := make([][]string, 0, len(out))
	for _, r := range out {
		rows = append(rows, []string{r.Phase, r.Kind, r.Name, r.ID})
	}
	p.Table([]string{"PHASE", "KIND", "NAME", "ID"}, rows)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d ruleset(s)\n", len(out))
	return nil
}
