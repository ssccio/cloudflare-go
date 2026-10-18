package tunnels

import (
	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	ingressAccount  string
	ingressHostname string
	ingressService  string
	ingressDryRun   bool
)

type ingressResult struct {
	ID       string `json:"id"       toon:"id"`
	Hostname string `json:"hostname" toon:"hostname"`
	Service  string `json:"service"  toon:"service"`
	Version  int64  `json:"version"  toon:"version"`
}

var ingressCmd = &cobra.Command{
	Use:   "ingress <tunnel-id>",
	Short: "Replace a remotely managed tunnel's ingress with one hostname → service rule",
	Long: `Replace the ingress of a remotely managed tunnel so --hostname goes to
--service and everything else gets 404. Running connectors pick up the new
config without a restart.

Examples:
  cf tunnels ingress TUNNEL_ID --account ACCOUNT_ID --hostname ledger.example.dev \
    --service http://localhost:8788 --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runIngress,
}

func init() {
	ingressCmd.Flags().StringVar(&ingressAccount, "account", "", "Account ID (required)")
	ingressCmd.Flags().StringVar(&ingressHostname, "hostname", "", "Public hostname (required)")
	ingressCmd.Flags().StringVar(&ingressService, "service", "", "Origin service, e.g. http://localhost:8080 (required)")
	ingressCmd.Flags().BoolVar(&ingressDryRun, "dry-run", false, "Show what would change without calling the API")
	for _, f := range []string{"account", "hostname", "service"} {
		_ = ingressCmd.MarkFlagRequired(f)
	}
}

func runIngress(cmd *cobra.Command, args []string) error {
	p, token := cmdutil.Setup(cmd)
	id := args[0]

	if cmdutil.DryRun(p, ingressDryRun, "set tunnel %s ingress to %s → %s (everything else 404)",
		id, ingressHostname, ingressService) {
		return nil
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Setting ingress %s → %s…", ingressHostname, ingressService)
	res, err := cfClient.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(cmd.Context(), id,
		zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			AccountID: cf.F(ingressAccount),
			Config: cf.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
				Ingress: cf.F([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
					{Hostname: cf.F(ingressHostname), Service: cf.F(ingressService)},
					{Service: cf.F("http_status:404")},
				}),
			}),
		})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := ingressResult{ID: id, Hostname: ingressHostname, Service: ingressService, Version: res.Version}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Ingress updated")
	p.KV([][2]string{
		{"Tunnel ID", result.ID},
		{"Hostname", result.Hostname},
		{"Service", result.Service},
	})
	return nil
}
