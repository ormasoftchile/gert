// Package icm provides a Go client for the Microsoft ICM OData API.
//
// Auth: Uses Azure CLI token by default (az login).
// The user must have the ICM "Readers" role for search/query.
//
// Base URL: https://prod.microsofticm.com/api/cert
package icm

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production ICM API.
	DefaultBaseURL = "https://prod.microsofticm.com"

	// ICM resource ID for token acquisition.
	icmResource = "https://prod.microsofticm.com"
)

// Client is a lightweight ICM OData client.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

	// token cache
	token       string
	tokenExpiry time.Time
}

// New creates an ICM client with default settings.
// Forces HTTP/1.1 since ICM rejects HTTP/2.
func New() *Client {
	transport := &http.Transport{
		TLSNextProto: make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}
}

// Search queries incidents using an OData $filter expression.
// Returns up to `top` incidents ordered by CreateDate descending.
func (c *Client) Search(filter string, top int) ([]Incident, error) {
	if top <= 0 {
		top = 20
	}

	params := url.Values{}
	params.Set("$filter", filter)
	params.Set("$orderby", "CreateDate desc")
	params.Set("$top", fmt.Sprintf("%d", top))

	uri := fmt.Sprintf("%s/api/cert/incidents?%s", c.BaseURL, params.Encode())

	body, err := c.doGet(uri)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var result SearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("search: parse response: %w", err)
	}

	// Flatten custom fields
	for i := range result.Value {
		flattenCustomFields(&result.Value[i])
	}

	return result.Value, nil
}

// Get retrieves a single incident by ID with expanded custom fields.
func (c *Client) Get(incidentId int64) (*Incident, error) {
	uri := fmt.Sprintf("%s/api/cert/incidents(%d)?$expand=CustomFieldGroups($expand=CustomFields),OccuringLocation", c.BaseURL, incidentId)

	body, err := c.doGet(uri)
	if err != nil {
		return nil, fmt.Errorf("get(%d): %w", incidentId, err)
	}

	// OData v3 wraps in "d", OData v4 returns directly — try both
	var incident Incident
	if err := json.Unmarshal(body, &incident); err != nil {
		// Try wrapped format
		var wrapped GetResult
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return nil, fmt.Errorf("get(%d): parse: %w\nraw: %s", incidentId, err, truncate(body, 500))
		}
		if wrapped.D == nil {
			return nil, fmt.Errorf("get(%d): empty response", incidentId)
		}
		incident = *wrapped.D
	}

	flattenCustomFields(&incident)
	return &incident, nil
}

// doGet performs an authenticated GET request.
func (c *Client) doGet(uri string) ([]byte, error) {
	token, err := c.getToken()
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}

	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(body, 300))
	}

	return body, nil
}

// getToken returns a cached or fresh AAD token for the ICM resource.
// Priority:
//  1. ICM_TOKEN env var (manual injection — useful when az CLI can't acquire)
//  2. az account get-access-token (works when the ICM resource is registered)
func (c *Client) getToken() (string, error) {
	// Return cached token if still valid (with 2 min buffer)
	if c.token != "" && time.Now().Before(c.tokenExpiry.Add(-2*time.Minute)) {
		return c.token, nil
	}

	// 1. Check env var
	if t := envToken(); t != "" {
		c.token = t
		c.tokenExpiry = time.Now().Add(50 * time.Minute)
		return c.token, nil
	}

	// 2. Try az CLI
	cmd := exec.Command("az", "account", "get-access-token",
		"--resource", icmResource,
		"--tenant", "72f988bf-86f1-41af-91ab-2d7cd011db47",
		"--query", "accessToken",
		"--output", "tsv",
	)
	out, err := cmd.Output()
	if err != nil {
		var hint string
		if exitErr, ok := err.(*exec.ExitError); ok {
			hint = string(exitErr.Stderr)
		}
		return "", fmt.Errorf("az account get-access-token failed: %w\n%s\nSet ICM_TOKEN env var or ensure az login with ICM access", err, hint)
	}

	c.token = strings.TrimSpace(string(out))
	c.tokenExpiry = time.Now().Add(50 * time.Minute) // AAD tokens are typically valid for 60-90 min

	return c.token, nil
}
func envToken() string {
	if t := os.Getenv("ICM_TOKEN"); t != "" {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "Bearer ")
		t = strings.TrimPrefix(t, "bearer ")
		return t
	}
	return ""
}

// flattenCustomFields populates incident.Fields from the nested CustomFieldGroups.
// ICM returns: CustomFieldGroups → []{ GroupName, CustomFields → []{ Name, StringValue, ... } }
func flattenCustomFields(inc *Incident) {
	if inc.Fields == nil {
		inc.Fields = make(map[string]string)
	}
	for _, group := range inc.CustomFieldGroups {
		for _, cf := range group.Fields {
			if cf.Name != "" {
				val := cf.StringValue
				if val == "" && cf.NumberValue != nil {
					val = fmt.Sprintf("%v", *cf.NumberValue)
				}
				if val == "" && cf.BooleanValue != nil {
					val = fmt.Sprintf("%v", *cf.BooleanValue)
				}
				inc.Fields[cf.Name] = val
			}
		}
	}
}

func truncate(b []byte, max int) string {
	s := string(b)
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
