package tunnels

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createAccount   string
	createHostname  string
	createService   string
	createTokenFile string
	createDryRun    bool
)

type createResult struct {
	ID        string `json:"id"         toon:"id"`
	Name      string `json:"name"       toon:"name"`
	Hostname  string `json:"hostname"   toon:"hostname"`
	Service   string `json:"service"    toon:"service"`
	CNAME     string `json:"cname"      toon:"cname"`
	TokenFile string `json:"token_file" toon:"token_file"`
}

var createCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a remotely managed tunnel routing one hostname to a local service",
	Long: `Create a tunnel managed from the Cloudflare dashboard, set its ingress to
send --hostname to --service (everything else gets 404), and write the run
token to --token-file (mode 0600, never overwritten). The token is not printed.

Route DNS separately with a proxied CNAME to the printed target, then run:
  cloudflared tunnel run --token-file FILE

Examples:
  cf tunnels create ledger --account ACCOUNT_ID --hostname ledger.example.dev \
    --service http://localhost:8080 --token-file ~/.cloudflared/ledger.token --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createAccount, "account", "", "Account ID (required)")
	createCmd.Flags().StringVar(&createHostname, "hostname", "", "Public hostname (required)")
	createCmd.Flags().StringVar(&createService, "service", "", "Origin service, e.g. http://localhost:8080 (required)")
	createCmd.Flags().StringVar(&createTokenFile, "token-file", "", "File to write the run token to; must not exist (required)")
	createCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show what would be created without calling the API")
	for _, f := range []string{"account", "hostname", "service", "token-file"} {
		_ = createCmd.MarkFlagRequired(f)
	}
}

func runCreate(cmd *cobra.Command, args []string) error {
	p, token := cmdutil.Setup(cmd)
	name := args[0]

	if cmdutil.DryRun(p, createDryRun, "create tunnel %q in account %s routing %s → %s, token to %s",
		name, createAccount, createHostname, createService, createTokenFile) {
		return nil
	}

	// Claim the token file first so a bad path fails before anything is created.
	f, err := os.OpenFile(createTokenFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		p.Error("%v", err)
		return err
	}
	wrote := false
	defer func() {
		f.Close()
		if !wrote {
			os.Remove(createTokenFile)
		}
	}()

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}
	ctx := cmd.Context()

	p.Info("Creating tunnel %q…", name)
	tun, err := cfClient.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID: cf.F(createAccount),
		Name:      cf.F(name),
		ConfigSrc: cf.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	p.Info("Setting ingress %s → %s…", createHostname, createService)
	_, err = cfClient.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tun.ID,
		zero_trust.TunnelCloudflaredConfigurationUpdateParams{
			AccountID: cf.F(createAccount),
			Config: cf.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
				Ingress: cf.F([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
					{Hostname: cf.F(createHostname), Service: cf.F(createService)},
					{Service: cf.F("http_status:404")},
				}),
			}),
		})
	if err != nil {
		p.Error("tunnel %s created but ingress failed: %v", tun.ID, err)
		return err
	}

	runToken, err := cfClient.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, tun.ID,
		zero_trust.TunnelCloudflaredTokenGetParams{AccountID: cf.F(createAccount)})
	if err != nil {
		p.Error("tunnel %s created but token fetch failed: %v", tun.ID, err)
		return err
	}
	if _, err := fmt.Fprint(f, *runToken); err != nil {
		p.Error("write %s: %v", createTokenFile, err)
		return err
	}
	wrote = true

	result := createResult{
		ID:        tun.ID,
		Name:      name,
		Hostname:  createHostname,
		Service:   createService,
		CNAME:     tun.ID + ".cfargotunnel.com",
		TokenFile: createTokenFile,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Tunnel created")
	p.KV([][2]string{
		{"Tunnel ID", result.ID},
		{"Hostname", result.Hostname},
		{"Service", result.Service},
		{"CNAME target", result.CNAME},
		{"Token file", result.TokenFile},
	})
	return nil
}
