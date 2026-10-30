package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/kyledickey/shed/internal/auth"
)

// grafanaProxy returns the handler of /grafana/, which serves the Grafana
// at addr as the signed-in user, or redirects to the Telemetry page while
// telemetry is off. It runs behind RequirePage and the origin check.
func (s *Server) grafanaProxy(addr string) http.Handler {
	target := &url.URL{Scheme: "http", Host: addr}
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// Grafana compares the Origin of a mutation with Host.
			pr.Out.Host = pr.In.Host
			for name := range pr.Out.Header {
				if strings.HasPrefix(strings.ToUpper(name), "X-WEBAUTH-") {
					pr.Out.Header.Del(name)
				}
			}
			u, _ := auth.UserFrom(pr.In.Context())
			pr.Out.Header.Set("X-WEBAUTH-USER", u.Login)
			pr.Out.Header.Set("X-WEBAUTH-NAME", u.Name)
			withoutShedCookies(pr.Out)
		},
		// shed's security headers stand; Grafana's copies would duplicate
		// them.
		ModifyResponse: func(resp *http.Response) error {
			for _, sh := range shedHeaders {
				resp.Header.Del(sh.name)
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("proxy to Grafana", "path", r.URL.Path, "err", err)
			http.Error(w, "Grafana is not answering; see Server → Telemetry", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		on, err := s.control.TelemetryEnabled(r.Context())
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if !on {
			http.Redirect(w, r, "/server/telemetry", http.StatusFound)
			return
		}
		// A WebSocket handshake is a GET, so the mutation check passes it;
		// it gets the same origin check here.
		if r.Header.Get("Upgrade") != "" && !sameOrigin(s.baseURL, r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "untrusted request origin"})
			return
		}
		rp.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether r comes from a page of the dashboard origin.
func sameOrigin(baseURL string, r *http.Request) bool {
	u, err := url.Parse(baseURL)
	return err == nil && r.Header.Get("Origin") == u.Scheme+"://"+u.Host
}

// withoutShedCookies drops shed's own cookies, the session among them, so
// that Grafana never sees them.
func withoutShedCookies(r *http.Request) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, c := range cookies {
		if !strings.HasPrefix(c.Name, "shed_") {
			r.AddCookie(c)
		}
	}
}
