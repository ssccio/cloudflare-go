package r2

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// The SDK has no R2 object calls, so these use the REST endpoints directly.
const apiBase = "https://api.cloudflare.com/client/v4"

var (
	objPrefix string
	objLimit  int
	getKey    string
	getRaw    bool
)

func init() {
	objectsCmd.Flags().StringVar(&objPrefix, "prefix", "", "Only keys that start with this prefix")
	objectsCmd.Flags().IntVar(&objLimit, "limit", 1000, "Stop after this many objects")
	getCmd.Flags().StringVar(&getKey, "key", "", "Object key (required)")
	getCmd.Flags().BoolVar(&getRaw, "raw", false, "Do not decompress gzip data")
	_ = getCmd.MarkFlagRequired("key")
	Cmd.AddCommand(objectsCmd)
	Cmd.AddCommand(getCmd)
}

// ObjectRow is one object in a bucket listing.
type ObjectRow struct {
	Key          string `json:"key"           toon:"key"`
	Size         int64  `json:"size"          toon:"size"`
	LastModified string `json:"last_modified" toon:"last_modified"`
}

func r2Get(ctx context.Context, token, path string, q url.Values) (*http.Response, error) {
	u := apiBase + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

var objectsCmd = &cobra.Command{
	Use:   "objects",
	Short: "List objects in a bucket",
	Long: `List objects in the bucket in key order, with size and last-modified time.

Examples:
  cf r2 objects --account ACCOUNT_ID --bucket my-bucket --prefix logs/20261102/`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		path := fmt.Sprintf("/accounts/%s/r2/buckets/%s/objects", c.AccountID, url.PathEscape(bucket))
		rows := []ObjectRow{}
		cursor := ""
		for len(rows) < objLimit {
			q := url.Values{"per_page": {"1000"}}
			if objPrefix != "" {
				q.Set("prefix", objPrefix)
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			resp, err := r2Get(cmd.Context(), c.Token, path, q)
			if err != nil {
				p.Error("API error: %v", err)
				return err
			}
			var page struct {
				Result     []ObjectRow `json:"result"`
				ResultInfo struct {
					Cursor      string `json:"cursor"`
					IsTruncated bool   `json:"is_truncated"`
				} `json:"result_info"`
			}
			err = json.NewDecoder(resp.Body).Decode(&page)
			resp.Body.Close()
			if err != nil {
				p.Error("decoding response: %v", err)
				return err
			}
			rows = append(rows, page.Result...)
			if !page.ResultInfo.IsTruncated || page.ResultInfo.Cursor == "" {
				break
			}
			cursor = page.ResultInfo.Cursor
		}
		if len(rows) > objLimit {
			rows = rows[:objLimit]
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No objects match.")
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.Key, fmt.Sprint(r.Size), r.LastModified})
		}
		p.Table([]string{"KEY", "SIZE", "LAST MODIFIED"}, table)
		return nil
	},
}

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Write an object's contents to stdout",
	Long: `Write an object's contents to stdout. Gzip data is decompressed
unless --raw is set.

Examples:
  cf r2 get --account ACCOUNT_ID --bucket my-bucket --key logs/file.log.gz | head -1 | jq .`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		path := fmt.Sprintf("/accounts/%s/r2/buckets/%s/objects/%s", c.AccountID, url.PathEscape(bucket), url.PathEscape(getKey))
		resp, err := r2Get(cmd.Context(), c.Token, path, nil)
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		defer resp.Body.Close()
		// The HTTP client already undoes Content-Encoding: gzip, so check
		// the gzip magic bytes instead of trusting the .gz suffix.
		br := bufio.NewReader(resp.Body)
		var body io.Reader = br
		if magic, _ := br.Peek(2); !getRaw && len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
			zr, err := gzip.NewReader(br)
			if err != nil {
				p.Error("decompressing: %v", err)
				return err
			}
			defer zr.Close()
			body = zr
		}
		_, err = io.Copy(os.Stdout, body)
		return err
	},
}
