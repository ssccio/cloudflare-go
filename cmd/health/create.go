package health

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/healthchecks"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createZone        string
	createDomain      string
	createName        string
	createAddress     string
	createType        string
	createDescription string
	createDryRun      bool
)

var validHealthcheckTypes = []string{"HTTP", "HTTPS", "TCP"}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a health check in a zone",
	Long: `Create a new health check in the specified Cloudflare zone.

Supported types: HTTP, HTTPS, TCP

Examples:
  cf health create --zone ZONE_ID --name origin --address 203.0.113.1
  cf health create --domain example.com --name origin --address origin.example.com --type HTTP
  cf health create --domain example.com --name origin --address origin.example.com --dry-run
  cf health create --domain example.com --name origin --address origin.example.com --json`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createZone, "zone", "", "Zone ID")
	createCmd.Flags().StringVar(&createDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createCmd.Flags().StringVar(&createName, "name", "", "Health check name (required)")
	createCmd.Flags().StringVar(&createAddress, "address", "", "Hostname or IP address of the origin to check (required)")
	createCmd.Flags().StringVar(&createType, "type", "HTTPS", "Health check type: HTTP, HTTPS, TCP")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Optional description for the health check")
	createCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show what would be created without making the API call")

	createCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("address")
}

func runCreate(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, createZone, createDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	checkType := strings.ToUpper(createType)
	if !isValidHealthcheckType(checkType) {
		err := fmt.Errorf("invalid --type %q; valid values: %s", createType, strings.Join(validHealthcheckTypes, ", "))
		p.Error("%v", err)
		return err
	}

	if cmdutil.DryRun(p, createDryRun, "create %s health check %q for %s in zone %s", checkType, createName, createAddress, c.ZoneID) {
		return nil
	}

	p.Info("Creating %s health check %q for %s in zone %s…", checkType, createName, createAddress, c.ZoneID)

	resp, apiErr := c.Client.Healthchecks.New(
		cmd.Context(),
		healthchecks.HealthcheckNewParams{
			ZoneID: cf.F(c.ZoneID),
			QueryHealthcheck: healthchecks.QueryHealthcheckParam{
				Name:        cf.F(createName),
				Address:     cf.F(createAddress),
				Type:        cf.F(checkType),
				Description: cf.F(createDescription),
				Suspended:   cf.F(false),
			},
		},
	)
	if apiErr != nil {
		p.Error("API error: %v", apiErr)
		return apiErr
	}

	result := healthcheckResult{
		ID:          resp.ID,
		Name:        resp.Name,
		Address:     resp.Address,
		Type:        resp.Type,
		Enabled:     !resp.Suspended,
		Status:      string(resp.Status),
		Description: resp.Description,
		Interval:    resp.Interval,
		Timeout:     resp.Timeout,
		Retries:     resp.Retries,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Health check created")
	p.KV([][2]string{
		{"ID", result.ID},
		{"Name", result.Name},
		{"Address", result.Address},
		{"Type", result.Type},
	})
	return nil
}

func isValidHealthcheckType(t string) bool {
	for _, v := range validHealthcheckTypes {
		if t == v {
			return true
		}
	}
	return false
}
