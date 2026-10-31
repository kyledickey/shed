package observability

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"slices"
	"strconv"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// Labels on exporter containers.
const (
	// LabelExporter names, by service ID, the database an exporter
	// container watches.
	LabelExporter = "shed.exporter"
	// labelExporterSpec holds the hash of the spec an exporter was created
	// from.
	labelExporterSpec = "shed.exporter.spec"
)

// exporter is the Prometheus exporter that watches one kind of database.
type exporter struct {
	// image is the pinned reference, tag and digest.
	image string
	// port is the port the exporter serves /metrics on.
	port int
	// args returns the exporter's flags; statements reports whether the
	// database was started with query statistics.
	args func(target string, statements bool) []string
	// env returns the exporter's credentials as KEY=VALUE, from the
	// database's resolved variables.
	env func(target string, vars map[string]string) []string
}

// exportersByKind maps database kinds to their exporters. The images are bumped
// with shed releases.
var exportersByKind = map[string]exporter{
	"postgres": {
		image: "prometheuscommunity/postgres-exporter:v0.20.1@sha256:ac5ec343104fae0e2d84a27bb8d69b38430a11910c5382cad85d478d2bab713e",
		port:  9187,
		args: func(_ string, statements bool) []string {
			if !statements {
				return nil
			}
			return []string{"--collector.stat_statements", "--collector.stat_statements.include_query"}
		},
		env: func(target string, vars map[string]string) []string {
			user := cmp.Or(vars["POSTGRES_USER"], "postgres")
			db := cmp.Or(vars["POSTGRES_DB"], user)
			return []string{
				"DATA_SOURCE_URI=" + target + "/" + url.PathEscape(db) + "?sslmode=disable",
				"DATA_SOURCE_USER=" + user,
				"DATA_SOURCE_PASS=" + vars["POSTGRES_PASSWORD"],
			}
		},
	},
	"mysql": {
		image: "prom/mysqld-exporter:v0.21.0@sha256:afad0c59a154fea24ca5ea0e6f95f2b0b168601b111266963dc8a347715b5318",
		port:  9104,
		args: func(target string, _ bool) []string {
			return []string{
				"--mysqld.address=" + target,
				"--mysqld.username=root",
				"--collect.perf_schema.eventsstatements",
				"--collect.info_schema.innodb_metrics",
			}
		},
		env: func(_ string, vars map[string]string) []string {
			return []string{"MYSQLD_EXPORTER_PASSWORD=" + vars["MYSQL_ROOT_PASSWORD"]}
		},
	},
	"mongo": {
		image: "percona/mongodb_exporter:0.54.0@sha256:934e9db87e85e0fa63654e73c3504845993a254e898783ca905e65a68848b5a4",
		port:  9216,
		args:  func(string, bool) []string { return []string{"--collect-all"} },
		env: func(target string, vars map[string]string) []string {
			u := url.URL{Scheme: "mongodb", Host: target, Path: "/admin"}
			if name := vars["MONGO_INITDB_ROOT_USERNAME"]; name != "" {
				u.User = url.UserPassword(name, vars["MONGO_INITDB_ROOT_PASSWORD"])
			}
			return []string{"MONGODB_URI=" + u.String()}
		},
	},
	"redis": {
		image: "oliver006/redis_exporter:v1.96.0@sha256:3d57b9eb142a62ffc600063606a41cd471cd53d81bed10be5b56b3a8918f2c26",
		port:  9121,
		env: func(target string, vars map[string]string) []string {
			return []string{"REDIS_ADDR=" + target, "REDIS_PASSWORD=" + vars["REDIS_PASSWORD"]}
		},
	},
}

// database is a database service with an active deployment.
type database struct {
	svc     store.Service
	project store.Project
	dep     store.Deployment
}

// statements reports whether the active deployment was started with the
// template's arguments, which turn query statistics on.
func (d database) statements() bool {
	t, ok := catalog.Lookup(d.svc.Kind)
	return ok && d.svc.StartCommand == "" && d.dep.Runtime != nil && slices.Equal(d.dep.Runtime.Cmd, t.Cmd)
}

// network returns the name of the database's project network. It is the
// name the deployer gives the network.
func (d database) network() string { return "shed-" + d.svc.ProjectID }

// target returns the address the exporter connects to.
func (d database) target() string {
	port := d.dep.Port
	if t, ok := catalog.Lookup(d.svc.Kind); ok && port <= 0 {
		port = t.Port
	}
	return net.JoinHostPort(d.svc.Name, strconv.Itoa(port))
}

// exporterName returns the name of the exporter container of service serviceID.
func exporterName(serviceID string) string { return "shed-exp-" + serviceID }

// exporterSpec returns the container that watches d, labeled with the hash
// of everything it depends on. Credentials are passed as environment
// variables, never as arguments.
func exporterSpec(d database) docker.RunSpec {
	x := exportersByKind[d.svc.Kind]
	target := d.target()
	var vars map[string]string
	if d.dep.Runtime != nil {
		vars = d.dep.Runtime.Env
	}
	env := x.env(target, vars)
	slices.Sort(env)
	s := docker.RunSpec{
		Name:    exporterName(d.svc.ID),
		Image:   x.image,
		Env:     env,
		Network: d.network(),
		CPUs:    0.1,
		Memory:  64 << 20,
	}
	if x.args != nil {
		s.Cmd = x.args(target, d.statements())
	}
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	s.Labels = map[string]string{LabelExporter: d.svc.ID, labelExporterSpec: hex.EncodeToString(sum[:8])}
	return s
}

// scrapeTarget is one exporter VictoriaMetrics scrapes.
type scrapeTarget struct {
	// Addr is the exporter's host:port on the project network.
	Addr   string
	Labels map[string]string
}

// targetFor returns the scrape target of d's exporter at ip.
func targetFor(d database, ip string) scrapeTarget {
	return scrapeTarget{
		Addr: net.JoinHostPort(ip, strconv.Itoa(exportersByKind[d.svc.Kind].port)),
		Labels: map[string]string{
			"shed_project_id":    d.svc.ProjectID,
			"shed_project_name":  d.project.Name,
			"shed_service_id":    d.svc.ID,
			"shed_service_name":  d.svc.Name,
			"shed_service_kind":  d.svc.Kind,
			"shed_deployment_id": d.dep.ID,
			"shed_source":        "exporter",
		},
	}
}
