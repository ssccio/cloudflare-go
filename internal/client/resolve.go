package client

import (
	"context"
	"fmt"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"
)

// ResolveZoneID returns a zone ID directly if zoneID is non-empty, otherwise
// looks up the zone by domain name. Exactly one of zoneID or domain must be set.
func ResolveZoneID(ctx context.Context, c *cf.Client, zoneID, domain string) (string, error) {
	if zoneID != "" {
		return zoneID, nil
	}
	if domain == "" {
		return "", fmt.Errorf("one of --zone or --domain is required")
	}

	iter := c.Zones.ListAutoPaging(ctx, zones.ZoneListParams{
		Name: cf.F(domain),
	})
	for iter.Next() {
		return iter.Current().ID, nil
	}
	if err := iter.Err(); err != nil {
		return "", fmt.Errorf("looking up zone for %q: %w", domain, err)
	}
	return "", fmt.Errorf("no zone found for domain %q — check the name and token permissions", domain)
}
