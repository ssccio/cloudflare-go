package access

import (
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createAccount string
	createName    string
	createEmails  []string
	createSession string
	createDryRun  bool
)

type createResult struct {
	ID       string   `json:"id"        toon:"id"`
	Name     string   `json:"name"      toon:"name"`
	Domain   string   `json:"domain"    toon:"domain"`
	AUD      string   `json:"aud"       toon:"aud"`
	PolicyID string   `json:"policy_id" toon:"policy_id"`
	Allowed  []string `json:"allowed"   toon:"allowed"`
}

var appsCreateCmd = &cobra.Command{
	Use:   "create <domain>",
	Short: "Create a self-hosted Access application that allows only the given emails",
	Long: `Create an allow policy for --allow-email and a self-hosted Access application
on <domain> that uses it. Everyone else is denied. The printed AUD tag is what
the origin checks in the Cf-Access-Jwt-Assertion header.

Examples:
  cf access apps create ledger.example.dev --account ACCOUNT_ID \
    --allow-email someone@example.com --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runAppsCreate,
}

func init() {
	appsCreateCmd.Flags().StringVar(&createAccount, "account", "", "Account ID (required)")
	appsCreateCmd.Flags().StringVar(&createName, "name", "", "Application name (defaults to the domain)")
	appsCreateCmd.Flags().StringSliceVar(&createEmails, "allow-email", nil, "Email allowed in; repeat or comma-separate (required)")
	appsCreateCmd.Flags().StringVar(&createSession, "session-duration", "24h", "How long a login lasts")
	appsCreateCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show what would be created without calling the API")
	_ = appsCreateCmd.MarkFlagRequired("account")
	_ = appsCreateCmd.MarkFlagRequired("allow-email")
}

func runAppsCreate(cmd *cobra.Command, args []string) error {
	p, token := cmdutil.Setup(cmd)
	domain := args[0]
	name := createName
	if name == "" {
		name = domain
	}

	if cmdutil.DryRun(p, createDryRun, "create Access app %q on %s in account %s allowing %s",
		name, domain, createAccount, strings.Join(createEmails, ", ")) {
		return nil
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}
	ctx := cmd.Context()

	include := make([]zero_trust.AccessRuleUnionParam, 0, len(createEmails))
	for _, e := range createEmails {
		include = append(include, zero_trust.EmailRuleParam{
			Email: cf.F(zero_trust.EmailRuleEmailParam{Email: cf.F(e)}),
		})
	}

	p.Info("Creating allow policy for %s…", strings.Join(createEmails, ", "))
	pol, err := cfClient.ZeroTrust.Access.Policies.New(ctx, zero_trust.AccessPolicyNewParams{
		AccountID: cf.F(createAccount),
		Name:      cf.F(name + " allow"),
		Decision:  cf.F(zero_trust.DecisionAllow),
		Include:   cf.F(include),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	p.Info("Creating Access application on %s…", domain)
	app, err := cfClient.ZeroTrust.Access.Applications.New(ctx, zero_trust.AccessApplicationNewParams{
		AccountID: cf.F(createAccount),
		Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
			Domain:          cf.F(domain),
			Type:            cf.F(zero_trust.ApplicationTypeSelfHosted),
			Name:            cf.F(name),
			SessionDuration: cf.F(createSession),
			Policies: cf.F([]zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPolicyUnion{
				zero_trust.AccessApplicationNewParamsBodySelfHostedApplicationPoliciesAccessAppPolicyLink{
					ID:         cf.F(pol.ID),
					Precedence: cf.F(int64(1)),
				},
			}),
		},
	})
	if err != nil {
		p.Error("policy %s created but application failed: %v", pol.ID, err)
		return err
	}

	result := createResult{ID: app.ID, Name: name, Domain: domain, AUD: app.AUD, PolicyID: pol.ID, Allowed: createEmails}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Access application created")
	p.KV([][2]string{
		{"App ID", result.ID},
		{"Domain", result.Domain},
		{"AUD tag", result.AUD},
		{"Policy ID", result.PolicyID},
		{"Allowed", strings.Join(result.Allowed, ", ")},
	})
	return nil
}
