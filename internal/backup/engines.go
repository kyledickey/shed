package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/kyledickey/shed/internal/docker"
)

// engine describes how to dump and restore one kind of database. Scripts run
// with sh -c inside the database's own container, so credentials come from
// the container's environment and never appear on a command line.
type engine struct {
	// ext is the archive extension before ".zst".
	ext string
	// dump writes the dump to stdout.
	dump string
	// restore reads a dump from stdin. It is empty for redis, whose RDB file
	// is restored into the stopped service's volume instead.
	restore string
	// ready exits 0 once the database server, as PID 1 of the container,
	// accepts connections. It is needed only with restore.
	ready string
}

// Redis files: the catalog mounts the data volume at /data, and redis keeps
// its default file names. A restore writes the RDB file both as dump.rdb and
// as the base of a fresh multi-part AOF, since a server with appendonly
// enabled loads only the AOF.
const (
	redisRDBDir       = "/data"
	redisRDBFile      = "dump.rdb"
	redisAOFDir       = "appendonlydir"
	redisAOFBase      = "appendonly.aof.1.base.rdb"
	redisAOFManifest  = "appendonly.aof.manifest"
	redisManifestBody = "file " + redisAOFBase + " seq 1 type b\n"
)

// engines maps database kinds to their engines.
var engines = map[string]engine{
	"postgres": {
		ext: "sql",
		// The image trusts local socket connections; PGPASSWORD covers
		// images configured otherwise.
		dump: `export PGPASSWORD="$POSTGRES_PASSWORD"
exec pg_dumpall --clean --if-exists -U "${POSTGRES_USER:-postgres}"`,
		restore: postgresRestore,
		ready:   serverIs("postgres") + `pg_isready -q -U "${POSTGRES_USER:-postgres}"`,
	},
	"mysql": {
		ext: "sql",
		dump: `export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
exec mysqldump -uroot --all-databases --single-transaction --routines --events --triggers --set-gtid-purged=OFF`,
		restore: mysqlRestore,
		ready:   serverIs("mysqld") + `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqladmin -uroot ping >/dev/null`,
	},
	"mongo": {
		ext:     "archive",
		dump:    mongoAuth + `mongodump --quiet --archive "$@"`,
		restore: mongoAuth + mongoDropDatabases + `mongorestore --quiet --archive --drop "$@"`,
		ready: serverIs("mongod") + `mongosh --quiet --nodb --eval '
const r = new Mongo("mongodb://127.0.0.1:27017/?directConnection=true").getDB("admin").runCommand({ping: 1});
if (!r.ok) quit(1);' </dev/null`,
	},
	"redis": {
		ext:  "rdb",
		dump: redisDump,
	},
}

// serverIs returns a script line that fails unless PID 1 of the container
// is the program binary, so that a server the image's entrypoint runs while
// it initializes does not count as ready.
func serverIs(binary string) string {
	return `case "$(tr '\0' '\n' </proc/1/cmdline | head -n 1)" in ` + binary + `|*/` + binary + `) ;; *) exit 1 ;; esac
`
}

// postgresRestore loads a pg_dumpall --clean dump and stops at the first
// error. DROP DATABASE fails while other sessions use a database, and the
// statements after it would then mix the dump into the old data, so other
// sessions are terminated and new ones refused until the load is over. The
// dump drops and recreates only the databases it contains, so every
// database but postgres and the templates is dropped first: databases
// created after the backup do not survive the restore. The dump's DROP ROLE
// and CREATE ROLE of the connected user always fail and are left out.
const postgresRestore = `export PGPASSWORD="$POSTGRES_PASSWORD"
u="${POSTGRES_USER:-postgres}"
run() { psql -X -q -v ON_ERROR_STOP=1 -U "$u" -d postgres -o /dev/null "$@"; }
{
	cat <<'SQL'
SELECT format('ALTER DATABASE %I ALLOW_CONNECTIONS false', datname)
	FROM pg_database WHERE datallowconn AND datname NOT IN ('template0', current_database()) \gexec
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
	WHERE pid <> pg_backend_pid() AND datname IS NOT NULL;
SELECT format('DROP DATABASE %I WITH (FORCE)', datname)
	FROM pg_database WHERE datname NOT IN ('postgres', 'template0', 'template1') \gexec
SQL
	awk -v u="$u" 'BEGIN { q = "\"" u "\"" }
		/^\\connect / { body = 1 }
		!body && ($0 == "DROP ROLE IF EXISTS " u ";" || $0 == "DROP ROLE IF EXISTS " q ";" ||
			$0 == "CREATE ROLE " u ";" || $0 == "CREATE ROLE " q ";") { next }
		{ print }'
} | run
status=$?
run <<'SQL' || status=1
SELECT format('ALTER DATABASE %I ALLOW_CONNECTIONS true', datname)
	FROM pg_database WHERE NOT datallowconn AND datname <> 'template0' \gexec
SQL
exit $status`

// mysqlRestore loads a mysqldump --all-databases dump and stops at the first
// error. The dump creates its databases only if they do not exist and drops
// only the tables it contains, so every database but the system ones is
// dropped first: databases and tables created after the backup do not
// survive the restore. sys is kept, since mysqldump leaves it out.
const mysqlRestore = `export MYSQL_PWD="$MYSQL_ROOT_PASSWORD"
drops=$(mysql -uroot -N -B -r -e "SELECT CONCAT('DROP DATABASE ', CHAR(96 USING utf8mb4),
	REPLACE(schema_name, CHAR(96 USING utf8mb4), REPEAT(CHAR(96 USING utf8mb4), 2)), CHAR(96 USING utf8mb4), ';')
	FROM information_schema.schemata
	WHERE schema_name NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')" </dev/null) || exit 1
{ printf 'SET FOREIGN_KEY_CHECKS=0;\n%s\n' "$drops"; cat; } |
	mysql -uroot --init-command="SET SESSION lock_wait_timeout=300"`

// mongoDropDatabases drops every database but admin, config, and local before
// mongorestore, whose --drop drops only the collections in the archive:
// databases and collections created after the backup do not survive the
// restore. The shell reads the credentials from the environment, so they
// never appear on a command line.
const mongoDropDatabases = `mongosh --quiet --nodb --eval '
const m = new Mongo("mongodb://127.0.0.1:27017/?directConnection=true");
const admin = m.getDB("admin");
const user = process.env.MONGO_INITDB_ROOT_USERNAME;
if (user) admin.auth(user, process.env.MONGO_INITDB_ROOT_PASSWORD);
for (const d of admin.adminCommand({listDatabases: 1, nameOnly: true}).databases) {
	if (!["admin", "config", "local"].includes(d.name)) {
		const res = m.getDB(d.name).dropDatabase();
		if (!res.ok) throw new Error("drop " + d.name + ": " + JSON.stringify(res));
	}
}' </dev/null
`

// mongoAuth sets the positional parameters to the root credentials of the
// container, passing the password through a private config file.
const mongoAuth = `set -e
if [ -n "$MONGO_INITDB_ROOT_USERNAME" ]; then
	cfg=$(mktemp)
	trap 'rm -f "$cfg"' EXIT
	chmod 600 "$cfg"
	pw=$(printf '%s' "$MONGO_INITDB_ROOT_PASSWORD" | sed 's/[\\"]/\\&/g')
	printf 'password: "%s"\n' "$pw" > "$cfg"
	set -- --username "$MONGO_INITDB_ROOT_USERNAME" --authenticationDatabase admin --config "$cfg"
fi
`

// redisDump starts a background save once no other is running, waits for it
// to finish successfully, and writes the RDB file to stdout.
const redisDump = `set -e
[ -z "$REDIS_PASSWORD" ] || export REDISCLI_AUTH="$REDIS_PASSWORD"
info() { redis-cli INFO persistence | tr -d '\r' | sed -n "s/^$1://p"; }
fail() { echo "$*" >&2; exit 1; }
n=0
tick() { n=$((n+1)); [ "$n" -lt 18000 ] || fail "timed out waiting for the background save"; sleep 0.2; }
while :; do
	while [ "$(info rdb_bgsave_in_progress)" != 0 ]; do tick; done
	saves=$(info rdb_saves)
	[ -n "$saves" ] || fail "redis does not report rdb_saves; redis 7 or newer is required"
	out=$(redis-cli BGSAVE SCHEDULE 2>&1) || true
	case "$out" in
	"Background saving"*) break ;;
	*"already in progress"*) tick ;;
	*) fail "BGSAVE: $out" ;;
	esac
done
while [ "$(info rdb_bgsave_in_progress)" != 0 ] ||
	{ [ "$(info rdb_saves)" = "$saves" ] && [ "$(info rdb_last_bgsave_status)" = ok ]; }; do
	tick
done
[ "$(info rdb_last_bgsave_status)" = ok ] || fail "background save failed; see the redis logs"
cat ` + redisRDBDir + "/" + redisRDBFile

// isDatabase reports whether kind is a database with a dump engine.
func isDatabase(kind string) bool {
	_, ok := engines[kind]
	return ok
}

// stderrTail keeps the last bytes written to it, to explain a failed command.
type stderrTail struct {
	buf []byte
}

const stderrTailSize = 4 << 10

func (t *stderrTail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTailSize; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *stderrTail) String() string { return strings.TrimSpace(string(t.buf)) }

// execScript runs script with sh -c in container id and explains a failure
// with the end of its standard error.
func execScript(ctx context.Context, d Docker, id, what, script string, stdin io.Reader, stdout io.Writer) error {
	var stderr stderrTail
	err := d.Exec(ctx, id, []string{"sh", "-c", script}, stdin, stdout, &stderr)
	if err == nil {
		return nil
	}
	var exit *docker.ExitError
	if errors.As(err, &exit) && stderr.String() != "" {
		return fmt.Errorf("%s: exit status %d: %s", what, exit.Code, stderr.String())
	}
	return fmt.Errorf("%s: %w", what, err)
}
