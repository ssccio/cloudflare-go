// Package d1 implements the `cf d1` subcommand group: D1 database size and
// read-only SQL queries.
package d1

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/d1"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// MaxBytes is the D1 per-database size limit on the paid plan (10 GB).
const MaxBytes = 10_000_000_000

// Cmd is the `cf d1` parent command.
var Cmd = &cobra.Command{
	Use:   "d1",
	Short: "Read D1 databases",
	Long: `Show a D1 database's size against the 10 GB limit, and run read-only
SQL against it.

Needs Account → D1 → Read on the token.`,
}

var (
	account  string
	database string
)

func init() {
	Cmd.PersistentFlags().StringVar(&account, "account", "", "Account ID (or CLOUDFLARE_ACCOUNT_ID)")
	Cmd.PersistentFlags().StringVar(&database, "database", "", "Database ID (required)")
	_ = Cmd.MarkPersistentFlagRequired("database")
	Cmd.AddCommand(infoCmd)
	Cmd.AddCommand(queryCmd)
}

type dbInfo struct {
	Name        string  `json:"name"         toon:"name"`
	UUID        string  `json:"uuid"         toon:"uuid"`
	FileSize    int64   `json:"file_size"    toon:"file_size"`
	PercentFull float64 `json:"percent_full" toon:"percent_full"`
	NumTables   int64   `json:"num_tables"   toon:"num_tables"`
	CreatedAt   string  `json:"created_at"   toon:"created_at"`
}

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show a database's size and table count",
	Long: `Show the database's size and how close it is to the 10 GB limit. A full
database refuses every write.

Examples:
  cf d1 info --account ACCOUNT_ID --database DATABASE_ID`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		db, err := c.Client.D1.Database.Get(cmd.Context(), database, d1.DatabaseGetParams{AccountID: cf.F(c.AccountID)})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		info := dbInfo{Name: db.Name, UUID: db.UUID, FileSize: int64(db.FileSize),
			PercentFull: db.FileSize / MaxBytes * 100, NumTables: int64(db.NumTables),
			CreatedAt: db.CreatedAt.UTC().Format(time.RFC3339)}
		if p.JSON || p.TOON {
			p.PrintResult(info)
			return nil
		}
		p.KV([][2]string{
			{"Name", info.Name},
			{"UUID", info.UUID},
			{"Size", fmt.Sprintf("%d bytes (%.2f%% of 10 GB)", info.FileSize, info.PercentFull)},
			{"Tables", fmt.Sprint(info.NumTables)},
			{"Created", info.CreatedAt},
		})
		return nil
	},
}

var sqlText string

// writeWords matches any SQL keyword that can change a database. The check is
// deliberately blunt: a false refusal is fine, a false allow is not.
var writeWords = regexp.MustCompile(`(?i)\b(insert|update|delete|replace|upsert|drop|alter|create|attach|detach|vacuum|reindex|pragma|begin|commit|rollback|savepoint|release|analyze)\b`)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Run read-only SQL against a database",
	Long: `Run SELECT statements against the database. Any statement that contains a
keyword able to change data (INSERT, UPDATE, DELETE, DROP, PRAGMA and
others) is refused before it is sent. Several statements separated by
semicolons run as one batch and print one result set each.

Examples:
  cf d1 query --account ACCOUNT_ID --database DATABASE_ID \
    --sql 'SELECT max(processed_at) FROM processed_objects'`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		if err := checkReadOnly(sqlText); err != nil {
			p.Error("%v", err)
			return err
		}
		page, err := c.Client.D1.Database.Query(cmd.Context(), database, d1.DatabaseQueryParams{
			AccountID: cf.F(c.AccountID),
			Body:      d1.DatabaseQueryParamsBodyD1SingleQuery{Sql: cf.F(sqlText)},
		})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		sets := make([][]any, 0, len(page.Result))
		for _, r := range page.Result {
			sets = append(sets, r.Results)
		}
		// The SQL result shape is unknown ahead of time, so every mode prints
		// the raw result sets; --toon still applies.
		p.PrintResult(sets)
		return nil
	},
}

func init() {
	queryCmd.Flags().StringVar(&sqlText, "sql", "", "SQL to run (SELECT only)")
	_ = queryCmd.MarkFlagRequired("sql")
}

func checkReadOnly(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("--sql is empty")
	}
	if w := writeWords.FindString(sql); w != "" {
		return fmt.Errorf("refused: %q can change data; cf d1 query runs read-only SQL only", strings.ToUpper(w))
	}
	return nil
}
