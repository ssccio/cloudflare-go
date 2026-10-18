package zones

import (
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createAccount string
	createDryRun  bool
)

type createResult struct {
	ID          string   `json:"id"           toon:"id"`
	Name        string   `json:"name"         toon:"name"`
	Status      string   `json:"status"       toon:"status"`
	NameServers []string `json:"name_servers" toon:"name_servers"`
}

var createCmd = &cobra.Command{
	Use:   "create <domain>",
	Short: "Create a full DNS zone in an account",
	Long: `Create a full zone (DNS hosted on Cloudflare) and print the nameservers
Cloudflare assigned. The zone stays pending until the parent delegates to them.
A subdomain such as sb.example.dev needs an Enterprise plan on the parent.

Examples:
  cf zones create sb.example.dev --account ACCOUNT_ID --dry-run
  cf zones create sb.example.dev --account ACCOUNT_ID --json`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createAccount, "account", "", "Account ID to own the zone (required)")
	createCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show what would be created without calling the API")
	_ = createCmd.MarkFlagRequired("account")
}

func runCreate(cmd *cobra.Command, args []string) error {
	p, token := cmdutil.Setup(cmd)
	name := args[0]

	if cmdutil.DryRun(p, createDryRun, "create zone %s in account %s", name, createAccount) {
		return nil
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Creating zone %s…", name)
	z, err := cfClient.Zones.New(cmd.Context(), zones.ZoneNewParams{
		Account: cf.F(zones.ZoneNewParamsAccount{ID: cf.F(createAccount)}),
		Name:    cf.F(name),
		Type:    cf.F(zones.TypeFull),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := createResult{ID: z.ID, Name: z.Name, Status: string(z.Status), NameServers: z.NameServers}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Zone created; delegate to these nameservers to activate it")
	p.KV([][2]string{
		{"Zone ID", result.ID},
		{"Name", result.Name},
		{"Status", result.Status},
		{"Nameservers", strings.Join(result.NameServers, ", ")},
	})
	return nil
}
