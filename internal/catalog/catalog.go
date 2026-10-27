// Package catalog describes the database templates shed can provision:
// image, port, data directory, and default variables.
package catalog

import "crypto/rand"

// Template describes how to run one kind of database.
type Template struct {
	// Kind is the service kind, such as "postgres".
	Kind string
	// Image is the Docker image to run.
	Image string
	// Port is the port the database listens on.
	Port int
	// MountPath is where the persistent volume is mounted.
	MountPath string
	// Cmd overrides the image's default command. Nil keeps the default.
	// Every template's command turns on query statistics and the slow query
	// log, which telemetry reads.
	Cmd []string
	// Vars returns fresh default variables, including newly generated
	// passwords. Values may contain ${{ }} references.
	Vars func() map[string]string
}

// templates lists the available templates in display order.
var templates = []Template{
	{
		Kind:  "postgres",
		Image: "postgres:18-alpine",
		Port:  5432,
		// PostgreSQL 18 images keep data in a version-specific directory
		// under /var/lib/postgresql, so the volume must cover the parent.
		MountPath: "/var/lib/postgresql",
		Cmd: []string{
			"-c", "shared_preload_libraries=pg_stat_statements",
			"-c", "pg_stat_statements.track=all",
			"-c", "log_min_duration_statement=250",
		},
		Vars: func() map[string]string {
			return map[string]string{
				"POSTGRES_USER":     "postgres",
				"POSTGRES_PASSWORD": password(),
				"POSTGRES_DB":       "app",
				"DATABASE_URL":      "postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}",
			}
		},
	},
	{
		Kind:      "mysql",
		Image:     "mysql:9",
		Port:      3306,
		MountPath: "/var/lib/mysql",
		Cmd:       []string{"sh", "-c", mysqlScript},
		Vars: func() map[string]string {
			return map[string]string{
				"MYSQL_ROOT_PASSWORD": password(),
				"MYSQL_DATABASE":      "app",
				"MYSQL_URL":           "mysql://root:${{MYSQL_ROOT_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:3306/${{MYSQL_DATABASE}}",
				"DATABASE_URL":        "${{MYSQL_URL}}",
			}
		},
	},
	{
		Kind:      "mongo",
		Image:     "mongo:8",
		Port:      27017,
		MountPath: "/data/db",
		Cmd:       []string{"--profile=1", "--slowms=250"},
		Vars: func() map[string]string {
			return map[string]string{
				"MONGO_INITDB_ROOT_USERNAME": "mongo",
				"MONGO_INITDB_ROOT_PASSWORD": password(),
				"MONGO_URL":                  "mongodb://${{MONGO_INITDB_ROOT_USERNAME}}:${{MONGO_INITDB_ROOT_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:27017",
			}
		},
	},
	{
		Kind:      "redis",
		Image:     "redis:8-alpine",
		Port:      6379,
		MountPath: "/data",
		Cmd: []string{
			"sh", "-c",
			`exec redis-server --requirepass "$REDIS_PASSWORD" --appendonly yes --slowlog-log-slower-than 250000`,
		},
		Vars: func() map[string]string {
			return map[string]string{
				"REDIS_PASSWORD": password(),
				"REDIS_URL":      "redis://default:${{REDIS_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:6379",
			}
		},
	},
}

// mysqlScript starts MySQL with the slow query log on. mysqld cannot log to
// /dev/stderr or a pipe, only to a regular file, so a tail copies the file to
// the container's stderr, where the logs pipeline reads it, and the file is
// emptied once it passes 10 MiB.
const mysqlScript = `f=/tmp/slow.log
: >$f && chown mysql:mysql $f
tail -F $f >&2 &
(while sleep 60; do [ "$(stat -c %s $f)" -gt 10485760 ] && : >$f; done) &
exec docker-entrypoint.sh mysqld --performance-schema=ON --slow-query-log=ON --long-query-time=0.25 --slow-query-log-file=$f`

// Lookup returns the template for kind.
func Lookup(kind string) (Template, bool) {
	for _, t := range templates {
		if t.Kind == kind {
			return t, true
		}
	}
	return Template{}, false
}

// Kinds returns the available template kinds in display order.
func Kinds() []string {
	kinds := make([]string, len(templates))
	for i, t := range templates {
		kinds[i] = t.Kind
	}
	return kinds
}

const (
	passwordLen      = 24
	passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

// password returns a random alphanumeric password, which is safe to embed in
// URLs without escaping.
func password() string {
	// Reject bytes at or above the largest multiple of the alphabet size to
	// avoid modulo bias.
	const limit = 256 - 256%len(passwordAlphabet)
	out := make([]byte, 0, passwordLen)
	buf := make([]byte, passwordLen)
	for len(out) < passwordLen {
		rand.Read(buf) // Never returns an error.
		for _, b := range buf {
			if int(b) < limit && len(out) < passwordLen {
				out = append(out, passwordAlphabet[int(b)%len(passwordAlphabet)])
			}
		}
	}
	return string(out)
}
