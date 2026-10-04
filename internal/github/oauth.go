package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// User is a GitHub account that signed in through the App.
type User struct {
	ID        int64
	Login     string
	Name      string
	AvatarURL string
}

// AuthorizeURL returns the GitHub page that asks a user to authorize the App.
// GitHub redirects back to redirectURL with the code and state.
func (c *Client) AuthorizeURL(redirectURL, state string) string {
	q := url.Values{
		"client_id":    {c.app.ClientID},
		"redirect_uri": {redirectURL},
		"state":        {state},
	}
	return c.webURL + "/login/oauth/authorize?" + q.Encode()
}

// ExchangeUser trades the code from an authorization redirect for a user
// token and returns the user it belongs to. redirectURL must match the one
// given to [Client.AuthorizeURL].
func (c *Client) ExchangeUser(ctx context.Context, code, redirectURL string) (User, error) {
	token, err := c.exchangeCode(ctx, code, redirectURL)
	if err != nil {
		return User{}, err
	}
	client, err := api(c.http, c.apiURL, token)
	if err != nil {
		return User{}, fmt.Errorf("github: exchange user: %w", err)
	}
	u, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return User{}, fmt.Errorf("github: get authenticated user: %w", err)
	}
	return User{ID: u.GetID(), Login: u.GetLogin(), Name: u.GetName(), AvatarURL: u.GetAvatarURL()}, nil
}

// exchangeCode redeems an OAuth code for a user access token.
func (c *Client) exchangeCode(ctx context.Context, code, redirectURL string) (string, error) {
	form := url.Values{
		"client_id":     {c.app.ClientID},
		"client_secret": {c.app.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURL},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.webURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("github: exchange code: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: exchange code: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("github: exchange code: decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	if body.Error != "" {
		return "", fmt.Errorf("github: exchange code: %s: %s", body.Error, body.ErrorDescription)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("github: exchange code: no access token in response (HTTP %d)", resp.StatusCode)
	}
	return body.AccessToken, nil
}
