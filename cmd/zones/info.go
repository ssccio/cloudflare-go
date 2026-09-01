package zones

import (
	"fmt"
	"strings"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// zoneInfo is the detailed view of a single zone.
type zoneInfo struct {
	ID                  string   `json:"id"                    toon:"id"`
	Name                string   `json:"name"                  toon:"name"`
	Status              string   `json:"status"                toon:"status"`
	Paused              bool     `json:"paused"                toon:"paused"`
	Plan                string   `json:"plan"                  toon:"plan"`
	DevelopmentMode     float64  `json:"development_mode"      toon:"development_mode"`
	NameServers         []string `json:"name_servers"          toon:"name_servers"`
	OriginalNameServers []string `json:"original_name_servers" toon:"original_name_servers"`
	OwnerID             string   `json:"owner_id"              toon:"owner_id"`
	OwnerName           string   `json:"owner_name"            toon:"owner_name"`
	AccountID           string   `json:"account_id"            toon:"account_id"`
	AccountName         string   `json:"account_name"          toon:"account_name"`
	CreatedOn           string   `json:"created_on"            toon:"created_on"`
	ModifiedOn          string   `json:"modified_on"           toon:"modified_on"`
}

var (
	infoZone   string
	infoDomain string
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show detailed information about a zone",
	Long: `Show the full record for a single zone: status, plan, name servers,
owner, and timestamps.

Examples:
  cf zones info --domain example.com
  cf zones info --zone ZONE_ID --json`,
	RunE: runInfo,
}

func init() {
	infoCmd.Flags().StringVar(&infoZone, "zone", "", "Zone ID")
	infoCmd.Flags().StringVar(&infoDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	infoCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runInfo(cmd *cobra.Command, _ []string) error {
	p, token := cmdutil.Setup(cmd)

	if infoZone == "" && infoDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, infoZone, infoDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	z, err := cfClient.Zones.Get(cmd.Context(), zones.ZoneGetParams{ZoneID: cf.F(zoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	info := zoneInfo{
		ID:                  z.ID,
		Name:                z.Name,
		Status:              string(z.Status),
		Paused:              z.Paused,
		Plan:                z.Plan.Name,
		DevelopmentMode:     z.DevelopmentMode,
		NameServers:         z.NameServers,
		OriginalNameServers: z.OriginalNameServers,
		OwnerID:             z.Owner.ID,
		OwnerName:           z.Owner.Name,
		AccountID:           z.Account.ID,
		AccountName:         z.Account.Name,
		CreatedOn:           z.CreatedOn.Format(time.RFC3339),
		ModifiedOn:          z.ModifiedOn.Format(time.RFC3339),
	}

	if p.JSON || p.TOON {
		p.PrintResult(info)
		return nil
	}

	p.KV([][2]string{
		{"Name", info.Name},
		{"ID", info.ID},
		{"Status", info.Status},
		{"Paused", fmt.Sprintf("%t", info.Paused)},
		{"Plan", info.Plan},
		{"Dev Mode", fmt.Sprintf("%.0f", info.DevelopmentMode)},
		{"Name Servers", strings.Join(info.NameServers, ", ")},
		{"Original NS", strings.Join(info.OriginalNameServers, ", ")},
		{"Account", fmt.Sprintf("%s (%s)", info.AccountName, info.AccountID)},
		{"Owner", info.OwnerID},
		{"Created", info.CreatedOn},
		{"Modified", info.ModifiedOn},
	})
	return nil
}
