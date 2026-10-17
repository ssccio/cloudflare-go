// Package inventory implements `cf inventory`: a read-only dump of every
// zone's configuration in one JSON document, for loading into a local
// reference database.
package inventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/healthchecks"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	invZone   string
	invDomain string
)

// settingKeys are the zone settings recorded per zone.
var settingKeys = []string{"ssl", "min_tls_version", "always_use_https", "security_level"}

type inventory struct {
	GeneratedAt string          `json:"generated_at" toon:"generated_at"`
	Zones       []zoneInventory `json:"zones"        toon:"zones"`
}

type zoneInventory struct {
	ID              string            `json:"id"               toon:"id"`
	Name            string            `json:"name"             toon:"name"`
	Status          string            `json:"status"           toon:"status"`
	Plan            string            `json:"plan"             toon:"plan"`
	Settings        map[string]any    `json:"settings"         toon:"settings"`
	DNS             []dnsRecord       `json:"dns"              toon:"dns"`
	CustomHostnames []customHostname  `json:"custom_hostnames" toon:"custom_hostnames"`
	FallbackOrigin  string            `json:"fallback_origin,omitempty" toon:"fallback_origin,omitempty"`
	Rulesets        []ruleset         `json:"rulesets"         toon:"rulesets"`
	Logpush         []logpushJob      `json:"logpush"          toon:"logpush"`
	HealthChecks    []healthCheck     `json:"health_checks"    toon:"health_checks"`
	Errors          map[string]string `json:"errors,omitempty" toon:"errors,omitempty"`
}

type dnsRecord struct {
	ID      string `json:"id"      toon:"id"`
	Type    string `json:"type"    toon:"type"`
	Name    string `json:"name"    toon:"name"`
	Content string `json:"content" toon:"content"`
	Proxied bool   `json:"proxied" toon:"proxied"`
	TTL     int    `json:"ttl"     toon:"ttl"`
}

type customHostname struct {
	ID        string `json:"id"         toon:"id"`
	Hostname  string `json:"hostname"   toon:"hostname"`
	Status    string `json:"status"     toon:"status"`
	SSLStatus string `json:"ssl_status" toon:"ssl_status"`
}

type ruleset struct {
	ID    string `json:"id"    toon:"id"`
	Name  string `json:"name"  toon:"name"`
	Phase string `json:"phase" toon:"phase"`
	Kind  string `json:"kind"  toon:"kind"`
	Rules []rule `json:"rules" toon:"rules"`
}

type rule struct {
	ID          string `json:"id"          toon:"id"`
	Description string `json:"description" toon:"description"`
	Action      string `json:"action"      toon:"action"`
	Enabled     bool   `json:"enabled"     toon:"enabled"`
	Expression  string `json:"expression"  toon:"expression"`
}

type logpushJob struct {
	ID          int64  `json:"id"          toon:"id"`
	Name        string `json:"name"        toon:"name"`
	Dataset     string `json:"dataset"     toon:"dataset"`
	Enabled     bool   `json:"enabled"     toon:"enabled"`
	Destination string `json:"destination" toon:"destination"`
}

type healthCheck struct {
	ID      string `json:"id"      toon:"id"`
	Name    string `json:"name"    toon:"name"`
	Address string `json:"address" toon:"address"`
	Type    string `json:"type"    toon:"type"`
	Status  string `json:"status"  toon:"status"`
}

// Cmd is the `cf inventory` command.
var Cmd = &cobra.Command{
	Use:   "inventory",
	Short: "Dump every zone's configuration as one document (read-only)",
	Long: `Read the configuration of every zone the token can see, or one zone:
settings, DNS records, custom hostnames and fallback origin, rulesets with
their rules (every phase), Logpush jobs and health checks.

A section the token cannot read is recorded under the zone's "errors" key
instead of failing the run. Logpush destinations have their query string
removed, since it can carry credentials.

Examples:
  cf inventory --json > cf-inventory.json
  cf inventory --domain example.com --json
  cf inventory`,
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&invZone, "zone", "", "Zone ID (default: all zones)")
	Cmd.Flags().StringVar(&invDomain, "domain", "", "Domain name (default: all zones)")
	Cmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func run(cmd *cobra.Command, _ []string) error {
	p, token := cmdutil.Setup(cmd)
	c, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}
	ctx := cmd.Context()

	params := zones.ZoneListParams{}
	if invDomain != "" {
		params.Name = cf.F(invDomain)
	}
	var zs []zones.Zone
	iter := c.Zones.ListAutoPaging(ctx, params)
	for iter.Next() {
		if z := iter.Current(); invZone == "" || z.ID == invZone {
			zs = append(zs, z)
		}
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}
	if len(zs) == 0 {
		err := fmt.Errorf("no zones matched")
		p.Error("%v", err)
		return err
	}

	inv := inventory{GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	for i, z := range zs {
		p.Info("[%d/%d] %s…", i+1, len(zs), z.Name)
		inv.Zones = append(inv.Zones, collectZone(ctx, c, z))
	}

	if p.JSON || p.TOON {
		p.PrintResult(inv)
		return nil
	}
	printSummary(p, inv)
	return nil
}

func collectZone(ctx context.Context, c *cf.Client, z zones.Zone) zoneInventory {
	zi := zoneInventory{
		ID: z.ID, Name: z.Name, Status: string(z.Status), Plan: z.Plan.Name,
		Settings: map[string]any{}, Errors: map[string]string{},
	}
	zoneID := cf.F(z.ID)

	for _, key := range settingKeys {
		res, err := c.Zones.Settings.Get(ctx, key, zones.SettingGetParams{ZoneID: zoneID})
		if err != nil {
			zi.Errors["settings."+key] = err.Error()
			continue
		}
		zi.Settings[key] = res.Value
	}

	dnsIter := c.DNS.Records.ListAutoPaging(ctx, dns.RecordListParams{ZoneID: zoneID})
	for dnsIter.Next() {
		r := dnsIter.Current()
		zi.DNS = append(zi.DNS, dnsRecord{
			ID: r.ID, Type: string(r.Type), Name: r.Name, Content: r.Content, Proxied: r.Proxied, TTL: int(r.TTL),
		})
	}
	if err := dnsIter.Err(); err != nil {
		zi.Errors["dns"] = err.Error()
	}

	chIter := c.CustomHostnames.ListAutoPaging(ctx, custom_hostnames.CustomHostnameListParams{ZoneID: zoneID})
	for chIter.Next() {
		h := chIter.Current()
		zi.CustomHostnames = append(zi.CustomHostnames, customHostname{
			ID: h.ID, Hostname: h.Hostname, Status: string(h.Status), SSLStatus: string(h.SSL.Status),
		})
	}
	if err := chIter.Err(); err != nil {
		zi.Errors["custom_hostnames"] = err.Error()
	}
	if len(zi.CustomHostnames) > 0 {
		fo, err := c.CustomHostnames.FallbackOrigin.Get(ctx, custom_hostnames.FallbackOriginGetParams{ZoneID: zoneID})
		if err != nil {
			zi.Errors["fallback_origin"] = err.Error()
		} else {
			zi.FallbackOrigin = fo.Origin
		}
	}

	rsIter := c.Rulesets.ListAutoPaging(ctx, rulesets.RulesetListParams{ZoneID: zoneID})
	for rsIter.Next() {
		r := rsIter.Current()
		rs := ruleset{ID: r.ID, Name: r.Name, Phase: string(r.Phase), Kind: string(r.Kind)}
		// Managed rulesets are Cloudflare's own; zone rulesets hold our rules,
		// including the execute rules that deploy the managed ones.
		if r.Kind == rulesets.KindZone {
			full, err := c.Rulesets.Get(ctx, r.ID, rulesets.RulesetGetParams{ZoneID: zoneID})
			if err != nil {
				zi.Errors["ruleset."+r.ID] = err.Error()
			} else {
				for _, ru := range full.Rules {
					rs.Rules = append(rs.Rules, rule{
						ID: ru.ID, Description: ru.Description, Action: string(ru.Action), Enabled: ru.Enabled, Expression: ru.Expression,
					})
				}
			}
		}
		zi.Rulesets = append(zi.Rulesets, rs)
	}
	if err := rsIter.Err(); err != nil {
		zi.Errors["rulesets"] = err.Error()
	}

	lp, err := c.Logpush.Jobs.List(ctx, logpush.JobListParams{ZoneID: zoneID})
	if err != nil {
		zi.Errors["logpush"] = err.Error()
	} else {
		for _, j := range lp.Result {
			zi.Logpush = append(zi.Logpush, logpushJob{
				ID: j.ID, Name: j.Name, Dataset: string(j.Dataset), Enabled: j.Enabled, Destination: stripQuery(j.DestinationConf),
			})
		}
	}

	hcIter := c.Healthchecks.ListAutoPaging(ctx, healthchecks.HealthcheckListParams{ZoneID: zoneID})
	for hcIter.Next() {
		h := hcIter.Current()
		zi.HealthChecks = append(zi.HealthChecks, healthCheck{
			ID: h.ID, Name: h.Name, Address: h.Address, Type: h.Type, Status: string(h.Status),
		})
	}
	if err := hcIter.Err(); err != nil {
		zi.Errors["health_checks"] = err.Error()
	}

	return zi
}

// stripQuery drops a destination's query string, where Logpush keeps
// header auth tokens and storage keys.
func stripQuery(dest string) string {
	if i := strings.IndexByte(dest, '?'); i >= 0 {
		return dest[:i] + "?[redacted]"
	}
	return dest
}

func printSummary(p *output.Printer, inv inventory) {
	rows := make([][]string, 0, len(inv.Zones))
	for _, z := range inv.Zones {
		rules := 0
		for _, rs := range z.Rulesets {
			rules += len(rs.Rules)
		}
		rows = append(rows, []string{
			z.Name, z.Plan,
			fmt.Sprint(len(z.DNS)), fmt.Sprint(len(z.CustomHostnames)),
			fmt.Sprint(len(z.Rulesets)), fmt.Sprint(rules),
			fmt.Sprint(len(z.Logpush)), fmt.Sprint(len(z.HealthChecks)), fmt.Sprint(len(z.Errors)),
		})
	}
	p.Table([]string{"ZONE", "PLAN", "DNS", "HOSTNAMES", "RULESETS", "RULES", "LOGPUSH", "HEALTH", "ERRORS"}, rows)
}
