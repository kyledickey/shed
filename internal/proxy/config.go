package proxy

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/caddyserver/certmagic"
)

// obj is a JSON object.
type obj = map[string]any

// buildConfig returns the Caddy JSON configuration for cfg and routes. The
// result is deterministic: it does not depend on the order of routes.
func buildConfig(cfg Config, routes []Route) ([]byte, error) {
	if cfg.HTTPPort <= 0 || cfg.HTTPSPort <= 0 {
		return nil, fmt.Errorf("invalid ports: http %d, https %d", cfg.HTTPPort, cfg.HTTPSPort)
	}

	routes = slices.Clone(routes)
	for i := range routes {
		routes[i].Host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(routes[i].Host)), ".")
		if routes[i].Host == "" || routes[i].Upstream == "" {
			return nil, fmt.Errorf("invalid route %+v: host and upstream are required", routes[i])
		}
	}
	slices.SortFunc(routes, func(a, b Route) int {
		if c := strings.Compare(a.Host, b.Host); c != 0 {
			return c
		}
		return strings.Compare(a.Upstream, b.Upstream)
	})
	// Ambiguous hosts must never change routing based on upstream sort order.
	for i := 1; i < len(routes); i++ {
		if routes[i-1].Host == routes[i].Host {
			return nil, fmt.Errorf("duplicate route host %q", routes[i].Host)
		}
	}

	handlers := make([]obj, 0, len(routes)+1)
	var public, internal []string
	for _, r := range routes {
		handlers = append(handlers, obj{
			"match": []obj{{"host": []string{r.Host}}},
			"handle": []obj{{
				"handler":   "reverse_proxy",
				"upstreams": []obj{{"dial": r.Upstream}},
			}},
			"terminal": true,
		})
		if isPublicName(r.Host) {
			public = append(public, r.Host)
		} else {
			internal = append(internal, r.Host)
		}
	}
	handlers = append(handlers, obj{
		"handle":   []obj{{"handler": "static_response", "status_code": 404}},
		"terminal": true,
	})

	conf := obj{
		"admin":   obj{"disabled": true},
		"logging": obj{"logs": obj{"default": obj{"writer": logWriter(cfg.LogFile)}}},
		"apps": obj{
			"http": obj{
				"http_port":  cfg.HTTPPort,
				"https_port": cfg.HTTPSPort,
				"servers": obj{
					"shed": obj{
						"listen": []string{":" + strconv.Itoa(cfg.HTTPSPort)},
						"routes": handlers,
					},
				},
			},
			"pki": obj{
				// Never modify the host's trust store.
				"certificate_authorities": obj{"local": obj{"install_trust": false}},
			},
		},
	}
	if cfg.StorageDir != "" {
		conf["storage"] = obj{"module": "file_system", "root": cfg.StorageDir}
	}
	if policies := tlsPolicies(cfg.Email, public, internal); len(policies) > 0 {
		conf["apps"].(obj)["tls"] = obj{"automation": obj{"policies": policies}}
	}
	return json.Marshal(conf)
}

// isPublicName reports whether a public CA can issue a certificate for host.
// IP addresses and names like "localhost" cannot.
func isPublicName(host string) bool {
	return !certmagic.SubjectIsIP(host) && certmagic.SubjectQualifiesForPublicCert(host)
}

// tlsPolicies returns the certificate automation policies. Public names use
// ACME, with a contact email if one is configured. Other names get
// certificates from Caddy's internal CA, so they work without breaking ACME
// for the rest.
func tlsPolicies(email string, public, internal []string) []obj {
	var policies []obj
	if len(public) > 0 && email != "" {
		policies = append(policies, obj{
			"subjects": public,
			"issuers":  []obj{{"module": "acme", "email": email}},
		})
	}
	if len(internal) > 0 {
		policies = append(policies, obj{
			"subjects": internal,
			"issuers":  []obj{{"module": "internal"}},
		})
	}
	return policies
}

func logWriter(file string) obj {
	if file == "" {
		return obj{"output": "discard"}
	}
	return obj{"output": "file", "filename": file}
}
