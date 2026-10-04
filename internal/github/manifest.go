package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// manifest is the GitHub App manifest document.
type manifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	HookAttributes     hookAttributes    `json:"hook_attributes"`
	RedirectURL        string            `json:"redirect_url"`
	CallbackURLs       []string          `json:"callback_urls"`
	SetupURL           string            `json:"setup_url"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
}

type hookAttributes struct {
	URL string `json:"url"`
}

// Manifest returns the JSON App manifest for a shed instance served at
// baseURL, to be posted to GitHub's app creation page.
func Manifest(baseURL, name string) ([]byte, error) {
	base := strings.TrimRight(baseURL, "/")
	b, err := json.Marshal(manifest{
		Name:           name,
		URL:            base,
		HookAttributes: hookAttributes{URL: base + "/api/github/webhook"},
		RedirectURL:    base + "/api/setup/github/callback",
		CallbackURLs:   []string{base + "/api/auth/callback"},
		SetupURL:       base + "/",
		Public:         false,
		DefaultPermissions: map[string]string{
			"contents": "read", "metadata": "read", "checks": "read", "statuses": "read",
		},
		DefaultEvents: []string{"push"},
	})
	if err != nil {
		return nil, fmt.Errorf("github: encode manifest: %w", err)
	}
	return b, nil
}

// ConvertManifest exchanges the code GitHub returns after the manifest flow
// for the credentials of the newly created App. A nil httpClient means a
// client with a 30-second timeout.
func ConvertManifest(ctx context.Context, httpClient *http.Client, code string) (App, error) {
	return convertManifest(ctx, orDefault(httpClient), defaultAPIURL, code)
}

func convertManifest(ctx context.Context, hc *http.Client, apiURL, code string) (App, error) {
	client, err := api(hc, apiURL, "")
	if err != nil {
		return App{}, fmt.Errorf("github: convert manifest: %w", err)
	}
	cfg, _, err := client.Apps.CompleteAppManifest(ctx, code)
	if err != nil {
		return App{}, fmt.Errorf("github: convert manifest: %w", err)
	}
	return App{
		ID:            cfg.GetID(),
		Slug:          cfg.GetSlug(),
		ClientID:      cfg.GetClientID(),
		ClientSecret:  cfg.GetClientSecret(),
		WebhookSecret: cfg.GetWebhookSecret(),
		PrivateKey:    []byte(cfg.GetPEM()),
	}, nil
}
