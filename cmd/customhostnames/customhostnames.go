// Package customhostnames implements the `cf custom-hostnames` subcommand group.
package customhostnames

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"
	"github.com/cloudflare/cloudflare-go/v6/shared"
)

// Cmd is the `cf custom-hostnames` parent command.
var Cmd = &cobra.Command{
	Use:     "custom-hostnames",
	Aliases: []string{"ch"},
	Short:   "Manage Cloudflare custom hostnames (SSL for SaaS)",
	Long:    "Create, inspect, verify, and delete custom hostnames within a Cloudflare zone, plus the zone's fallback origin.",
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(getCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(updateCmd)
	Cmd.AddCommand(deleteCmd)
	Cmd.AddCommand(verifyCmd)
	Cmd.AddCommand(statusSummaryCmd)
	Cmd.AddCommand(fallbackOriginCmd)
	Cmd.AddCommand(setFallbackOriginCmd)
}

// validationRecord is one DCV record the customer must publish, in whichever
// form the certificate authority asked for it.
type validationRecord struct {
	TXTName  string   `json:"txt_name,omitempty"  toon:"txt_name,omitempty"`
	TXTValue string   `json:"txt_value,omitempty" toon:"txt_value,omitempty"`
	HTTPURL  string   `json:"http_url,omitempty"  toon:"http_url,omitempty"`
	HTTPBody string   `json:"http_body,omitempty" toon:"http_body,omitempty"`
	Emails   []string `json:"emails,omitempty"    toon:"emails,omitempty"`
}

func (v validationRecord) empty() bool {
	return v.TXTName == "" && v.TXTValue == "" && v.HTTPURL == "" && v.HTTPBody == "" && len(v.Emails) == 0
}

// ownershipRecord is the pre-validation record Cloudflare asks for to prove the
// customer controls the hostname.
type ownershipRecord struct {
	Name  string `json:"name"  toon:"name"`
	Type  string `json:"type"  toon:"type"`
	Value string `json:"value" toon:"value"`
}

// customHostnameResult is the serialisable result for --json / --toon output.
// JSON and TOON tags match so --query works the same in both modes.
type customHostnameResult struct {
	ID                   string             `json:"id"                     toon:"id"`
	Hostname             string             `json:"hostname"               toon:"hostname"`
	Status               string             `json:"status"                 toon:"status"`
	SSLStatus            string             `json:"ssl_status"             toon:"ssl_status"`
	SSLMethod            string             `json:"ssl_method"             toon:"ssl_method"`
	SSLType              string             `json:"ssl_type,omitempty"     toon:"ssl_type,omitempty"`
	CertificateAuthority string             `json:"certificate_authority,omitempty" toon:"certificate_authority,omitempty"`
	MinTLSVersion        string             `json:"min_tls_version,omitempty"       toon:"min_tls_version,omitempty"`
	CreatedAt            string             `json:"created_at,omitempty"   toon:"created_at,omitempty"`
	ValidationRecords    []validationRecord `json:"validation_records,omitempty"    toon:"validation_records,omitempty"`
	ValidationErrors     []string           `json:"validation_errors,omitempty"     toon:"validation_errors,omitempty"`
	VerificationErrors   []string           `json:"verification_errors,omitempty"   toon:"verification_errors,omitempty"`
	Ownership            *ownershipRecord   `json:"ownership_verification,omitempty" toon:"ownership_verification,omitempty"`
	OwnershipHTTP        *validationRecord  `json:"ownership_verification_http,omitempty" toon:"ownership_verification_http,omitempty"`
}

// fromListResponse converts a list/get item into the local result struct.
func fromListResponse(h custom_hostnames.CustomHostnameListResponse) customHostnameResult {
	r := customHostnameResult{
		ID:                   h.ID,
		Hostname:             h.Hostname,
		Status:               string(h.Status),
		SSLStatus:            string(h.SSL.Status),
		SSLMethod:            string(h.SSL.Method),
		SSLType:              string(h.SSL.Type),
		CertificateAuthority: string(h.SSL.CertificateAuthority),
		MinTLSVersion:        string(h.SSL.Settings.MinTLSVersion),
		VerificationErrors:   h.VerificationErrors,
	}
	if !h.CreatedAt.IsZero() {
		r.CreatedAt = h.CreatedAt.String()
	}
	for _, vr := range h.SSL.ValidationRecords {
		rec := validationRecord{
			TXTName:  vr.TXTName,
			TXTValue: vr.TXTValue,
			HTTPURL:  vr.HTTPURL,
			HTTPBody: vr.HTTPBody,
			Emails:   vr.Emails,
		}
		if !rec.empty() {
			r.ValidationRecords = append(r.ValidationRecords, rec)
		}
	}
	for _, ve := range h.SSL.ValidationErrors {
		if ve.Message != "" {
			r.ValidationErrors = append(r.ValidationErrors, ve.Message)
		}
	}
	if h.OwnershipVerification.Name != "" || h.OwnershipVerification.Value != "" {
		r.Ownership = &ownershipRecord{
			Name:  h.OwnershipVerification.Name,
			Type:  string(h.OwnershipVerification.Type),
			Value: h.OwnershipVerification.Value,
		}
	}
	if h.OwnershipVerificationHTTP.HTTPURL != "" {
		r.OwnershipHTTP = &validationRecord{
			HTTPURL:  h.OwnershipVerificationHTTP.HTTPURL,
			HTTPBody: h.OwnershipVerificationHTTP.HTTPBody,
		}
	}
	return r
}

// fromNewResponse converts a create response into the local result struct.
func fromNewResponse(h *custom_hostnames.CustomHostnameNewResponse) customHostnameResult {
	r := customHostnameResult{
		ID:                   h.ID,
		Hostname:             h.Hostname,
		Status:               string(h.Status),
		SSLStatus:            string(h.SSL.Status),
		SSLMethod:            string(h.SSL.Method),
		SSLType:              string(h.SSL.Type),
		CertificateAuthority: string(h.SSL.CertificateAuthority),
		MinTLSVersion:        string(h.SSL.Settings.MinTLSVersion),
		VerificationErrors:   h.VerificationErrors,
	}
	if !h.CreatedAt.IsZero() {
		r.CreatedAt = h.CreatedAt.String()
	}
	for _, vr := range h.SSL.ValidationRecords {
		rec := validationRecord{
			TXTName:  vr.TXTName,
			TXTValue: vr.TXTValue,
			HTTPURL:  vr.HTTPURL,
			HTTPBody: vr.HTTPBody,
			Emails:   vr.Emails,
		}
		if !rec.empty() {
			r.ValidationRecords = append(r.ValidationRecords, rec)
		}
	}
	for _, ve := range h.SSL.ValidationErrors {
		if ve.Message != "" {
			r.ValidationErrors = append(r.ValidationErrors, ve.Message)
		}
	}
	if h.OwnershipVerification.Name != "" || h.OwnershipVerification.Value != "" {
		r.Ownership = &ownershipRecord{
			Name:  h.OwnershipVerification.Name,
			Type:  string(h.OwnershipVerification.Type),
			Value: h.OwnershipVerification.Value,
		}
	}
	if h.OwnershipVerificationHTTP.HTTPURL != "" {
		r.OwnershipHTTP = &validationRecord{
			HTTPURL:  h.OwnershipVerificationHTTP.HTTPURL,
			HTTPBody: h.OwnershipVerificationHTTP.HTTPBody,
		}
	}
	return r
}

// fromEditResponse converts an edit response into the local result struct.
func fromEditResponse(h *custom_hostnames.CustomHostnameEditResponse) customHostnameResult {
	r := customHostnameResult{
		ID:                   h.ID,
		Hostname:             h.Hostname,
		Status:               string(h.Status),
		SSLStatus:            string(h.SSL.Status),
		SSLMethod:            string(h.SSL.Method),
		SSLType:              string(h.SSL.Type),
		CertificateAuthority: string(h.SSL.CertificateAuthority),
		MinTLSVersion:        string(h.SSL.Settings.MinTLSVersion),
		VerificationErrors:   h.VerificationErrors,
	}
	if !h.CreatedAt.IsZero() {
		r.CreatedAt = h.CreatedAt.String()
	}
	for _, vr := range h.SSL.ValidationRecords {
		rec := validationRecord{
			TXTName:  vr.TXTName,
			TXTValue: vr.TXTValue,
			HTTPURL:  vr.HTTPURL,
			HTTPBody: vr.HTTPBody,
			Emails:   vr.Emails,
		}
		if !rec.empty() {
			r.ValidationRecords = append(r.ValidationRecords, rec)
		}
	}
	for _, ve := range h.SSL.ValidationErrors {
		if ve.Message != "" {
			r.ValidationErrors = append(r.ValidationErrors, ve.Message)
		}
	}
	if h.OwnershipVerification.Name != "" || h.OwnershipVerification.Value != "" {
		r.Ownership = &ownershipRecord{
			Name:  h.OwnershipVerification.Name,
			Type:  string(h.OwnershipVerification.Type),
			Value: h.OwnershipVerification.Value,
		}
	}
	if h.OwnershipVerificationHTTP.HTTPURL != "" {
		r.OwnershipHTTP = &validationRecord{
			HTTPURL:  h.OwnershipVerificationHTTP.HTTPURL,
			HTTPBody: h.OwnershipVerificationHTTP.HTTPBody,
		}
	}
	return r
}

// findByHostname looks a custom hostname up by its FQDN. The SDK's Get takes a
// custom hostname ID, so callers that only know the hostname list with the
// Hostname filter and take the first match.
func findByHostname(ctx context.Context, c *cf.Client, zoneID, hostname string) (customHostnameResult, error) {
	iter := c.CustomHostnames.ListAutoPaging(ctx, custom_hostnames.CustomHostnameListParams{
		ZoneID:   cf.F(zoneID),
		Hostname: cf.F(hostname),
	})
	for iter.Next() {
		return fromListResponse(iter.Current()), nil
	}
	if err := iter.Err(); err != nil {
		return customHostnameResult{}, fmt.Errorf("looking up custom hostname %q: %w", hostname, err)
	}
	return customHostnameResult{}, fmt.Errorf("no custom hostname found for %q in zone %s", hostname, zoneID)
}

// parseSSLMethod validates the --ssl-method flag.
func parseSSLMethod(v string) (custom_hostnames.DCVMethod, error) {
	switch strings.ToLower(v) {
	case "http":
		return custom_hostnames.DCVMethodHTTP, nil
	case "txt":
		return custom_hostnames.DCVMethodTXT, nil
	case "cname":
		// The v6 SDK only declares http, txt and email constants, but the API
		// accepts cname and the field marshals as a plain string.
		return custom_hostnames.DCVMethod("cname"), nil
	default:
		return "", fmt.Errorf("invalid --ssl-method %q; valid values: http, txt, cname", v)
	}
}

// parseMinTLSVersion validates the --min-tls-version flag.
func parseMinTLSVersion(v string) (string, error) {
	switch v {
	case "1.0", "1.1", "1.2", "1.3":
		return v, nil
	default:
		return "", fmt.Errorf("invalid --min-tls-version %q; valid values: 1.0, 1.1, 1.2, 1.3", v)
	}
}

// parseCertificateAuthority validates the --certificate-authority flag.
func parseCertificateAuthority(v string) (shared.CertificateCA, error) {
	switch strings.ToLower(v) {
	case "lets_encrypt":
		return shared.CertificateCALetsEncrypt, nil
	case "google":
		return shared.CertificateCAGoogle, nil
	default:
		return "", fmt.Errorf("invalid --certificate-authority %q; valid values: lets_encrypt, google", v)
	}
}

// dash renders an empty value as an em dash for table output.
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
