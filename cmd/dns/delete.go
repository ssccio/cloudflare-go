package dns

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	deleteZone   string
	deleteDomain string
	deleteID     string
	deleteForce  bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a DNS record from a zone",
	Long: `Delete a DNS record by ID. Shows the record details and prompts for
confirmation before deleting. Use --force to skip the confirmation prompt.

Examples:
  cf dns delete --zone ZONE_ID --id RECORD_ID
  cf dns delete --domain example.com --id RECORD_ID
  cf dns delete --domain example.com --id RECORD_ID --force
  cf dns delete --domain example.com --id RECORD_ID --json`,
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteZone, "zone", "", "Zone ID")
	deleteCmd.Flags().StringVar(&deleteDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteCmd.Flags().StringVar(&deleteID, "id", "", "Record ID to delete (required)")
	deleteCmd.Flags().BoolVar(&deleteForce, "force", false, "Skip confirmation prompt")

	deleteCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteCmd.MarkFlagRequired("id")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	toonFlag, _ := cmd.Root().PersistentFlags().GetBool("toon")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")
	query, _ := cmd.Root().PersistentFlags().GetString("query")

	p := output.New(jsonFlag, toonFlag, quiet, noColor, query)

	if deleteZone == "" && deleteDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, deleteZone, deleteDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	// Fetch the record so we can show its details before confirming.
	p.Info("Fetching record %s…", deleteID)
	rec, err := cfClient.DNS.Records.Get(
		context.Background(),
		deleteID,
		dns.RecordGetParams{ZoneID: cf.F(zoneID)},
	)
	if err != nil {
		p.Error("record not found: %v", err)
		return err
	}

	// In machine mode, require --force to prevent accidental deletes from scripts.
	if (jsonFlag || toonFlag) && !deleteForce {
		err := fmt.Errorf("use --force to delete in --json/--toon mode")
		p.Error("%v", err)
		return err
	}

	// Show record details.
	if !quiet {
		fmt.Fprintf(cmd.OutOrStdout(), "\nRecord to delete:\n")
		p.KV([][2]string{
			{"ID", rec.ID},
			{"Name", rec.Name},
			{"Type", string(rec.Type)},
			{"Content", rec.Content},
			{"TTL", ttlDisplay(int(rec.TTL))},
			{"Proxied", fmt.Sprintf("%v", rec.Proxied)},
		})
		fmt.Fprintln(cmd.OutOrStdout())
	}

	// Confirm unless --force or machine mode.
	if !deleteForce {
		if !confirmDelete(cmd) {
			p.Info("Aborted.")
			return nil
		}
	}

	// Delete.
	_, err = cfClient.DNS.Records.Delete(
		context.Background(),
		deleteID,
		dns.RecordDeleteParams{ZoneID: cf.F(zoneID)},
	)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := map[string]string{"id": rec.ID, "deleted": "true"}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("DNS record deleted: %s (%s %s)", rec.Name, string(rec.Type), rec.Content)
	return nil
}

// confirmDelete prompts the user for yes/no confirmation.
// Returns true if the user confirms.
func confirmDelete(cmd *cobra.Command) bool {
	fmt.Fprint(cmd.OutOrStdout(), "Delete this record? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}
