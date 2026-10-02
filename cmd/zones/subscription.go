package zones

import (
	"encoding/json"
	"fmt"
	"sort"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	subscriptionZone   string
	subscriptionDomain string
)

var subscriptionCmd = &cobra.Command{
	Use:   "subscription",
	Short: "Show a zone's plan subscription and add-on components",
	Long: `Show the zone's subscription as the API returns it: rate plan, state,
billing period, and component values. Read-only, REST.

Needs Account → Billing → Read on the token. Enterprise add-ons such as
Bot Management do not appear here even when entitled; use "cf bots status"
for that.

Examples:
  cf zones subscription --domain example.com
  cf zones subscription --domain example.com --json`,
	RunE: runSubscription,
}

func init() {
	subscriptionCmd.Flags().StringVar(&subscriptionZone, "zone", "", "Zone ID")
	subscriptionCmd.Flags().StringVar(&subscriptionDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	subscriptionCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runSubscription(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, subscriptionZone, subscriptionDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	res, err := c.Client.Zones.Subscriptions.Get(cmd.Context(), zones.SubscriptionGetParams{
		ZoneID: cf.F(c.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	// Keep every field the API sent; component and plan fields vary by contract.
	sub := map[string]any{}
	if err := json.Unmarshal([]byte(res.JSON.RawJSON()), &sub); err != nil {
		p.Error("decoding response: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(map[string]any{"zone_id": c.ZoneID, "subscription": sub})
		return nil
	}

	keys := make([]string, 0, len(sub))
	for k := range sub {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := [][2]string{{"zone_id", c.ZoneID}}
	for _, k := range keys {
		switch v := sub[k].(type) {
		case map[string]any, []any:
			b, _ := json.Marshal(v)
			pairs = append(pairs, [2]string{k, string(b)})
		default:
			pairs = append(pairs, [2]string{k, fmt.Sprint(v)})
		}
	}
	p.KV(pairs)
	return nil
}
