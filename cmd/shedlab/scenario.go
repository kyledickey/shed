package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

//go:embed scenarios/*.toml
var scenarioFiles embed.FS

//go:embed generator.py
var generatorScript string

const generatorImage = "python:3.13-alpine"

// scenario is a setup a lab can be provisioned with.
type scenario struct {
	Name        string    `toml:"-"`
	Description string    `toml:"description"`
	CPUs        int       `toml:"cpus"`
	MemoryGB    int       `toml:"memory_gb"`
	DiskGB      int       `toml:"disk_gb"`
	Telemetry   bool      `toml:"telemetry"`
	Projects    []project `toml:"project"`
}

type project struct {
	Name     string    `toml:"name"`
	Services []service `toml:"service"`
}

// service is one service of a scenario. Count deploys that many copies, named
// <name>-1, <name>-2, and so on. Generate makes it a mock data generator of
// that kind, sending Rate records per second.
type service struct {
	Generate string            `toml:"generate"`
	Rate     int               `toml:"rate"`
	Name     string            `toml:"name"`
	Kind     string            `toml:"kind"`
	Image    string            `toml:"image"`
	Repo     string            `toml:"repo"`
	Branch   string            `toml:"branch"`
	Start    string            `toml:"start"`
	Port     int               `toml:"port"`
	Count    int               `toml:"count"`
	Domain   bool              `toml:"domain"`
	Vars     map[string]string `toml:"vars"`
}

func loadScenarios() ([]scenario, error) {
	entries, err := scenarioFiles.ReadDir("scenarios")
	if err != nil {
		return nil, err
	}
	var all []scenario
	for _, e := range entries {
		raw, err := scenarioFiles.ReadFile(path.Join("scenarios", e.Name()))
		if err != nil {
			return nil, err
		}
		s, err := parseScenario(raw, strings.TrimSuffix(e.Name(), ".toml"))
		if err != nil {
			return nil, err
		}
		all = append(all, s)
	}
	return all, nil
}

func parseScenario(raw []byte, name string) (scenario, error) {
	s := scenario{Name: name}
	if err := toml.NewDecoder(bytes.NewReader(raw)).DisallowUnknownFields().Decode(&s); err != nil {
		return s, fmt.Errorf("scenario %s: %w", name, err)
	}
	return s, nil
}

func parseScenarioFile(file, name string) (scenario, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return scenario{}, err
	}
	return parseScenario(raw, name)
}

func findScenario(name string) (scenario, error) {
	all, err := loadScenarios()
	if err != nil {
		return scenario{}, err
	}
	names := make([]string, len(all))
	for i, s := range all {
		if s.Name == name {
			return s, nil
		}
		names[i] = s.Name
	}
	return scenario{}, fmt.Errorf("no scenario %q; choose one of %s", name, strings.Join(names, ", "))
}

// needsGitHub reports whether the scenario deploys from a repository.
func (s scenario) needsGitHub() bool {
	return slices.ContainsFunc(s.Projects, func(p project) bool {
		return slices.ContainsFunc(p.Services, func(svc service) bool { return svc.Repo != "" })
	})
}

// expand returns the services with Count applied.
func (p project) expand() []service {
	var out []service
	for _, svc := range p.Services {
		if svc.Kind == "" {
			svc.Kind = "app"
		}
		if svc.Generate != "" {
			svc.Image = generatorImage
			svc.Start = `python -u -c "$GEN_SCRIPT"`
			svc.Vars = map[string]string{"GEN_KIND": svc.Generate, "GEN_RATE": strconv.Itoa(svc.Rate), "GEN_SCRIPT": generatorScript}
		}
		if svc.Count <= 1 {
			out = append(out, svc)
			continue
		}
		for i := 1; i <= svc.Count; i++ {
			c := svc
			c.Name = fmt.Sprintf("%s-%d", svc.Name, i)
			out = append(out, c)
		}
	}
	return out
}

func (s scenario) serviceCount() int {
	n := 0
	for _, p := range s.Projects {
		n += len(p.expand())
	}
	return n
}

// apiClient calls a lab's dashboard API with its session, the way the
// dashboard does.
type apiClient struct {
	base    string
	session string
}

func (l lab) api() apiClient {
	return apiClient{base: fmt.Sprintf("http://127.0.0.1:%d", l.Slot.dashPort()), session: l.Session}
}

func (a apiClient) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: a.session})
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// apply creates the scenario's projects and services through shed's real API.
// Each new service starts deploying as soon as it exists.
func (a apiClient) apply(ctx context.Context, p *progress, s scenario) error {
	if s.Telemetry {
		p.set("turning on telemetry")
		var status struct {
			Settings map[string]any `json:"settings"`
		}
		if err := a.do(ctx, http.MethodGet, "/api/telemetry", nil, &status); err != nil {
			return err
		}
		status.Settings["enabled"] = true
		if err := a.do(ctx, http.MethodPut, "/api/telemetry", status.Settings, nil); err != nil {
			return err
		}
	}
	for _, proj := range s.Projects {
		var created struct {
			ID string `json:"id"`
		}
		if err := a.do(ctx, http.MethodPost, "/api/projects", map[string]string{"name": proj.Name}, &created); err != nil {
			return err
		}
		for _, svc := range proj.expand() {
			p.set(proj.Name + "/" + svc.Name)
			if err := a.createService(ctx, created.ID, svc); err != nil {
				return fmt.Errorf("%s/%s: %w", proj.Name, svc.Name, err)
			}
		}
	}
	return nil
}

// waitForServices blocks until no service is still deploying, and fails if any
// didn't end up active.
func (a apiClient) waitForServices(ctx context.Context, p *progress, total int) error {
	var bad []string
	err := poll(ctx, 15*time.Minute, "services to deploy", func() error {
		var projects []struct {
			Name     string `json:"name"`
			Services []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"services"`
		}
		if err := a.do(ctx, http.MethodGet, "/api/projects", nil, &projects); err != nil {
			return err
		}
		active, pending := 0, 0
		bad = nil
		for _, proj := range projects {
			for _, svc := range proj.Services {
				switch svc.Status {
				case "active":
					active++
				case "deploying", "offline":
					pending++
				default:
					bad = append(bad, proj.Name+"/"+svc.Name+" is "+svc.Status)
				}
			}
		}
		p.set(fmt.Sprintf("%d of %d active", active, total))
		if pending > 0 {
			return fmt.Errorf("%d still deploying", pending)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d of %d services didn't start:\n%s", len(bad), total, strings.Join(bad, "\n"))
	}
	return nil
}

func (a apiClient) createService(ctx context.Context, projectID string, svc service) error {
	var created struct {
		ID string `json:"id"`
	}
	req := map[string]string{"name": svc.Name, "kind": svc.Kind, "repo": svc.Repo, "branch": svc.Branch, "image": svc.Image}
	if err := a.do(ctx, http.MethodPost, "/api/projects/"+projectID+"/services", req, &created); err != nil {
		return err
	}
	patch := map[string]any{}
	if svc.Start != "" {
		patch["startCommand"] = svc.Start
	}
	if svc.Port != 0 {
		patch["port"] = svc.Port
	}
	if svc.Domain {
		if err := a.do(ctx, http.MethodPost, "/api/services/"+created.ID+"/domains", map[string]string{}, nil); err != nil {
			return err
		}
	}
	if len(patch) > 0 {
		if err := a.do(ctx, http.MethodPatch, "/api/services/"+created.ID, patch, nil); err != nil {
			return err
		}
	}
	if len(svc.Vars) > 0 {
		if err := a.do(ctx, http.MethodPut, "/api/services/"+created.ID+"/variables", svc.Vars, nil); err != nil {
			return err
		}
	}
	if len(patch) == 0 && len(svc.Vars) == 0 {
		return nil
	}
	return a.do(ctx, http.MethodPost, "/api/services/"+created.ID+"/deployments", nil, nil)
}

var setupTokenPattern = regexp.MustCompile(`token[="':\s]+([A-Z2-7]{16,})`)

// parseSetupToken finds the one-time setup token in shed's log.
func parseSetupToken(log string) (string, bool) {
	matches := setupTokenPattern.FindAllStringSubmatch(log, -1)
	if len(matches) == 0 {
		return "", false
	}
	return matches[len(matches)-1][1], true
}
