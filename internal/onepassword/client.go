package onepassword

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client is a 1Password SCIM Bridge REST client.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient returns a new Client. baseURL may be either the server root
// (e.g. "https://your-bridge.example.com") or the full SCIM base
// (e.g. "https://provisioning.1password.com/scim/v2"); both forms are
// normalised to the server root and the /scim/v2 prefix is appended
// by the client internally.
func NewClient(baseURL, token string) *Client {
	base := strings.TrimRight(baseURL, "/")
	base = strings.TrimSuffix(base, "/scim/v2")
	return &Client{
		baseURL: base,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// --- Types ---

// User represents a 1Password Business member as returned by the SCIM bridge.
type User struct {
	ID       string  `json:"id"`
	UserName string  `json:"userName"`
	Name     *Name   `json:"name,omitempty"`
	Active   bool    `json:"active"`
	Roles    []Role  `json:"roles,omitempty"`
	Emails   []Email `json:"emails,omitempty"`
}

// Name holds a user's structured name fields.
type Name struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	Formatted  string `json:"formatted"`
}

// Role holds a SCIM role assignment.
type Role struct {
	Value   string `json:"value"`
	Display string `json:"display"`
	Primary bool   `json:"primary"`
}

// Email holds a SCIM email entry.
type Email struct {
	Value   string `json:"value"`
	Type    string `json:"type"`
	Primary bool   `json:"primary"`
}

// listResponse is the SCIM 2.0 ListResponse envelope.
type listResponse struct {
	TotalResults int    `json:"totalResults"`
	StartIndex   int    `json:"startIndex"`
	ItemsPerPage int    `json:"itemsPerPage"`
	Resources    []User `json:"Resources"`
}

// --- Methods ---

// Ping probes the SCIM ServiceProviderConfig endpoint to verify connectivity
// and token validity. Returns a non-nil error if the bridge is unreachable or
// the token is rejected.
func (c *Client) Ping(ctx context.Context) error {
	var out map[string]any
	return c.get(ctx, "/scim/v2/ServiceProviderConfig", &out)
}

// ListActiveUsers returns all active (non-suspended) members from the 1Password
// SCIM bridge. It paginates through all users and filters locally on u.Active;
// server-side filtering is intentionally omitted because the 1Password SCIM
// bridge does not reliably support the SCIM filter query parameter.
func (c *Client) ListActiveUsers(ctx context.Context) ([]User, error) {
	var all []User
	startIndex := 1
	pageSize := 100

	for {
		path := fmt.Sprintf("/scim/v2/Users?startIndex=%d&count=%d", startIndex, pageSize)
		var page listResponse
		if err := c.get(ctx, path, &page); err != nil {
			return nil, err
		}

		for _, u := range page.Resources {
			if u.Active {
				all = append(all, u)
			}
		}

		// Stop when we've consumed all results.
		fetched := startIndex + len(page.Resources) - 1
		if len(page.Resources) == 0 || fetched >= page.TotalResults {
			break
		}
		startIndex += len(page.Resources)
	}

	return all, nil
}

// --- HTTP helper ---

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/scim+json, application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("1password SCIM GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("1password SCIM GET %s: 401 Unauthorized — check onepassword.api_token", path)
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("1password SCIM GET %s: 403 Forbidden — token may lack required scope", path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("1password SCIM GET %s: status %d", path, resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
