package core

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// This file builds the commands that dump, list, drop, create and import a
// database. They run inside the database's own container (docker exec), so the
// client tools always match the server version and credentials come from that
// container's environment ($POSTGRES_USER, $MARIADB_ROOT_PASSWORD, …): the app
// never reads, stores or logs a database password.
//
// Every command is `sh -c <script> sh <args…>`: names reach the script as
// positional parameters, never spliced into shell text, and are also restricted
// to a safe character set before they get here.

// Database engines a Database source can use.
const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
	EngineMariaDB  = "mariadb"
)

// dbNamePattern is what the app accepts as a database name: letters, digits,
// "_", "$" and "-", as every engine allows without quoting surprises.
var dbNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_$-]{0,63}$`)

// charsetPattern guards charset/collation/locale names read back from a server
// before they are put in a CREATE DATABASE.
var charsetPattern = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)

func validateDBName(name string) error {
	if !dbNamePattern.MatchString(name) {
		return fmt.Errorf("database name %q is not supported (use letters, digits, _, $ or -)", name)
	}
	return nil
}

// validEngine reports whether engine is one the app can dump and restore.
func validEngine(engine string) bool {
	switch engine {
	case EnginePostgres, EngineMySQL, EngineMariaDB:
		return true
	}
	return false
}

// engineLabel is the human name of an engine.
func engineLabel(engine string) string {
	switch engine {
	case EnginePostgres:
		return "PostgreSQL"
	case EngineMySQL:
		return "MySQL"
	case EngineMariaDB:
		return "MariaDB"
	}
	return engine
}

// detectEngine infers a database engine from a container: Coolify's own label
// for standalone databases first, then the image name. "" means not a database
// the app can dump.
func detectEngine(image string, labels map[string]string) string {
	switch strings.ToLower(labels["coolify.database.subType"]) {
	case "postgresql", "postgres":
		return EnginePostgres
	case "mysql":
		return EngineMySQL
	case "mariadb":
		return EngineMariaDB
	}
	img := strings.ToLower(image)
	if i := strings.IndexAny(img, "@"); i >= 0 {
		img = img[:i]
	}
	repo := img
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	base := path.Base(repo)
	switch {
	case strings.Contains(base, "mariadb"):
		return EngineMariaDB
	case strings.Contains(base, "mysql") || strings.Contains(base, "percona"):
		return EngineMySQL
	case strings.Contains(base, "postgres") || strings.Contains(base, "postgis") ||
		strings.Contains(base, "timescaledb") || strings.Contains(base, "pgvector"):
		return EnginePostgres
	}
	return ""
}

// defaultDatabaseOf is the database a container was created with, from the
// official images' environment (what an app on it most likely uses).
func defaultDatabaseOf(engine string, env []string) string {
	switch engine {
	case EnginePostgres:
		if v := envValue(env, "POSTGRES_DB"); v != "" {
			return v
		}
		if v := envValue(env, "POSTGRES_USER"); v != "" {
			return v
		}
		return "postgres"
	case EngineMySQL, EngineMariaDB:
		if v := envValue(env, "MARIADB_DATABASE"); v != "" {
			return v
		}
		return envValue(env, "MYSQL_DATABASE")
	}
	return ""
}

// systemDatabases are server-internal databases never offered for backup.
var systemDatabases = map[string]map[string]bool{
	EnginePostgres: {"postgres": true, "template0": true, "template1": true},
	EngineMySQL:    {"information_schema": true, "performance_schema": true, "mysql": true, "sys": true},
	EngineMariaDB:  {"information_schema": true, "performance_schema": true, "mysql": true, "sys": true},
}

// isSystemDatabase reports whether name is an engine-internal database.
func isSystemDatabase(engine, name string) bool { return systemDatabases[engine][name] }

// dumpFileName is the file a database dump is stored as inside its snapshot.
func dumpFileName(database string) string { return database + ".sql" }

// dbSnapshotTag marks a snapshot as a database dump of an engine, e.g.
// "resticweb-db:postgres", so a restore can tell what it holds.
func dbSnapshotTag(engine string) string { return dbTagPrefix + engine }

const (
	dbTagPrefix   = "resticweb-db:"
	preRestoreTag = "resticweb-prerestore"
)

// snapshotDBEngine returns the engine a database snapshot was dumped from, or
// "" if the snapshot is not a database dump.
func snapshotDBEngine(tags []string) string {
	for _, t := range tags {
		if e, ok := strings.CutPrefix(t, dbTagPrefix); ok && validEngine(e) {
			return e
		}
	}
	return ""
}

func shCmd(script string, args ...string) []string {
	return append([]string{"sh", "-c", script, "sh"}, args...)
}

// pgPrelude picks the superuser the official image was initialized with. The
// image trusts local socket connections, so no password is needed.
const pgPrelude = `U="${POSTGRES_USER:-postgres}"
`

// mysqlPrelude picks the client tools (MariaDB 11 images ship only mariadb*,
// MySQL images only mysql*) and the most privileged account the container's
// environment knows. The password goes through MYSQL_PWD, so it is not visible
// in the process list.
const mysqlPrelude = `if command -v mariadb >/dev/null 2>&1; then CLI=mariadb; else CLI=mysql; fi
if command -v mariadb-dump >/dev/null 2>&1; then DUMP=mariadb-dump; else DUMP=mysqldump; fi
U=root
PW="${MARIADB_ROOT_PASSWORD:-$MYSQL_ROOT_PASSWORD}"
if [ -z "$PW" ] && [ -n "${MARIADB_ROOT_PASSWORD_FILE:-$MYSQL_ROOT_PASSWORD_FILE}" ]; then PW="$(cat "${MARIADB_ROOT_PASSWORD_FILE:-$MYSQL_ROOT_PASSWORD_FILE}")"; fi
if [ -z "$PW" ] && [ -n "${MARIADB_USER:-$MYSQL_USER}" ]; then U="${MARIADB_USER:-$MYSQL_USER}"; PW="${MARIADB_PASSWORD:-$MYSQL_PASSWORD}"; fi
export MYSQL_PWD="$PW"
`

// listDatabasesCmd prints one database name per line.
func listDatabasesCmd(engine string) ([]string, error) {
	switch engine {
	case EnginePostgres:
		return shCmd(pgPrelude + `exec psql -U "$U" -d postgres -AtX -c "SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY 1"`), nil
	case EngineMySQL, EngineMariaDB:
		return shCmd(mysqlPrelude + `exec "$CLI" -u"$U" -N -B -e 'SHOW DATABASES'`), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// dumpCmd writes a plain SQL dump of one database to stdout. The dump has no
// CREATE DATABASE or USE statement, so it imports into any database name.
func dumpCmd(engine, database string) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	switch engine {
	case EnginePostgres:
		return shCmd(pgPrelude+`exec pg_dump -U "$U" --no-password --format=plain -d "$1"`, database), nil
	case EngineMySQL, EngineMariaDB:
		// --events needs the EVENT privilege, which only root reliably has.
		return shCmd(mysqlPrelude+`EV=""; [ "$U" = root ] && EV="--events"
exec "$DUMP" -u"$U" --single-transaction --quick --routines --triggers $EV --hex-blob --no-tablespaces --default-character-set=utf8mb4 "$1"`, database), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// dbMeta is what a database is recreated with after a drop, so a restore keeps
// its owner and character set. Exists is false when there is no such database;
// empty fields fall back to the server's defaults.
type dbMeta struct {
	Exists   bool
	Owner    string // postgres
	Encoding string // postgres
	Collate  string // postgres LC_COLLATE, mysql collation
	Ctype    string // postgres LC_CTYPE
	Charset  string // mysql
}

// dbMetaCmd prints one "|"-separated line describing the database, or nothing
// if it does not exist.
func dbMetaCmd(engine, database string) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	switch engine {
	case EnginePostgres:
		return shCmd(pgPrelude+`exec psql -U "$U" -d postgres -AtX -v ON_ERROR_STOP=1 -v db="$1" <<'SQL'
SELECT concat_ws('|', pg_get_userbyid(datdba), pg_encoding_to_char(encoding), coalesce(datcollate, ''), coalesce(datctype, '')) FROM pg_database WHERE datname = :'db';
SQL`, database), nil
	case EngineMySQL, EngineMariaDB:
		q := "SELECT CONCAT(DEFAULT_CHARACTER_SET_NAME, '|', DEFAULT_COLLATION_NAME) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = '" + database + "'"
		return shCmd(mysqlPrelude+`exec "$CLI" -u"$U" -N -B -e "$1"`, q), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// parseDBMeta reads dbMetaCmd's output.
func parseDBMeta(engine, out string) dbMeta {
	line := strings.TrimSpace(out)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return dbMeta{}
	}
	parts := strings.Split(line, "|")
	field := func(i int) string {
		if i < len(parts) && charsetPattern.MatchString(parts[i]) {
			return parts[i]
		}
		return ""
	}
	switch engine {
	case EnginePostgres:
		m := dbMeta{Exists: true, Encoding: field(1), Collate: field(2), Ctype: field(3)}
		if len(parts) > 0 && dbNamePattern.MatchString(parts[0]) {
			m.Owner = parts[0]
		}
		return m
	case EngineMySQL, EngineMariaDB:
		return dbMeta{Exists: true, Charset: field(0), Collate: field(1)}
	}
	return dbMeta{Exists: true}
}

// dropCmd drops the database if it exists. A database in use cannot be
// dropped, so its sessions are ended first: on PostgreSQL new connections are
// refused and existing ones terminated; on MySQL/MariaDB they are killed, and
// the DROP waits at most a minute for a lock rather than the default year.
func dropCmd(engine, database string) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	switch engine {
	case EnginePostgres:
		return shCmd(pgPrelude+`exec psql -U "$U" -d postgres -qAtX -v ON_ERROR_STOP=1 -v db="$1" <<'SQL' >/dev/null
SELECT format('ALTER DATABASE %I WITH ALLOW_CONNECTIONS false', :'db') FROM pg_database WHERE datname = :'db' \gexec
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = :'db' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS :"db";
SQL`, database), nil
	case EngineMySQL, EngineMariaDB:
		sessions := "SELECT id FROM information_schema.PROCESSLIST WHERE db = '" + database + "' AND id <> CONNECTION_ID()"
		drop := "SET SESSION lock_wait_timeout = 60; DROP DATABASE IF EXISTS `" + database + "`"
		return shCmd(mysqlPrelude+`for id in $("$CLI" -u"$U" -N -B -e "$1"); do "$CLI" -u"$U" -e "KILL $id" 2>/dev/null; done
exec "$CLI" -u"$U" -e "$2"`, sessions, drop), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// pgAllowConnectionsCmd lets clients connect to a database again: undoes the
// first step of a PostgreSQL drop that did not go through.
func pgAllowConnectionsCmd(database string) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	return shCmd(pgPrelude+`exec psql -U "$U" -d postgres -qAtX -v ON_ERROR_STOP=1 -v db="$1" <<'SQL' >/dev/null
SELECT format('ALTER DATABASE %I WITH ALLOW_CONNECTIONS true', :'db') FROM pg_database WHERE datname = :'db' \gexec
SQL`, database), nil
}

// pgCopyPropertiesCmd copies what a plain pg_dump does not carry from one
// database to another: its settings (ALTER DATABASE … SET, also per role), its
// privileges and its comment. A restore imports into a fresh database, so
// without this they would be lost.
func pgCopyPropertiesCmd(from, to string) ([]string, error) {
	if err := validateDBName(from); err != nil {
		return nil, err
	}
	if err := validateDBName(to); err != nil {
		return nil, err
	}
	return shCmd(pgPrelude+`exec psql -U "$U" -d postgres -qAtX -v ON_ERROR_STOP=1 -v from="$1" -v to="$2" <<'SQL' >/dev/null
SELECT CASE WHEN s.setrole = 0
         THEN format('ALTER DATABASE %I SET %s = %L', :'to', split_part(c, '=', 1), substr(c, strpos(c, '=') + 1))
         ELSE format('ALTER ROLE %I IN DATABASE %I SET %s = %L', pg_get_userbyid(s.setrole), :'to', split_part(c, '=', 1), substr(c, strpos(c, '=') + 1))
       END
FROM pg_db_role_setting s, unnest(s.setconfig) c
WHERE s.setdatabase = (SELECT oid FROM pg_database WHERE datname = :'from') \gexec
UPDATE pg_database SET datacl = (SELECT datacl FROM pg_database WHERE datname = :'from') WHERE datname = :'to';
SELECT format('COMMENT ON DATABASE %I IS %L', :'to', shobj_description(oid, 'pg_database'))
FROM pg_database WHERE datname = :'from' AND shobj_description(oid, 'pg_database') IS NOT NULL \gexec
SQL`, from, to), nil
}

// pgSwapCmd puts a restored database in place of the live one: the live one is
// closed to new connections, its sessions ended and it is dropped, then the
// restored one takes its name. Without a live database (restore under a new
// name) it is just the rename. The gap without a database is a moment.
func pgSwapCmd(restored, database string, replace bool) ([]string, error) {
	if err := validateDBName(restored); err != nil {
		return nil, err
	}
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	drop := ""
	if replace {
		drop = `SELECT format('ALTER DATABASE %I WITH ALLOW_CONNECTIONS false', :'db') FROM pg_database WHERE datname = :'db' \gexec
SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = :'db' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS :"db";
`
	}
	return shCmd(pgPrelude+`exec psql -U "$U" -d postgres -qAtX -v ON_ERROR_STOP=1 -v db="$1" -v tmp="$2" <<'SQL' >/dev/null
`+drop+`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = :'tmp' AND pid <> pg_backend_pid();
ALTER DATABASE :"tmp" RENAME TO :"db";
SQL`, database, restored), nil
}

// dumpComplete reports whether a dump ends with its tool's completion marker.
// A dump cut short (the tool killed, the stream broken) lacks it.
func dumpComplete(engine string, tail []byte) bool {
	switch engine {
	case EnginePostgres:
		return bytes.Contains(tail, []byte("-- PostgreSQL database dump complete"))
	case EngineMySQL, EngineMariaDB:
		return bytes.Contains(tail, []byte("-- Dump completed"))
	}
	return false
}

// createCmd creates an empty database, with the owner and character set from
// meta when it described the database that was dropped.
func createCmd(engine, database string, meta dbMeta) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	switch engine {
	case EnginePostgres:
		// psql variables quote every value (:"x" as an identifier, :'x' as a
		// literal); only the clause keywords are chosen here.
		vars := []string{"-v", `db="$1"`}
		args := []string{database}
		stmt := `CREATE DATABASE :"db"`
		add := func(clause, name, value string) {
			if value == "" {
				return
			}
			args = append(args, value)
			vars = append(vars, "-v", fmt.Sprintf(`%s="$%d"`, name, len(args)))
			stmt += " " + clause
		}
		if meta.Exists {
			add(`OWNER :"owner"`, "owner", meta.Owner)
			if meta.Encoding != "" && meta.Collate != "" && meta.Ctype != "" {
				stmt += " TEMPLATE template0"
				add(`ENCODING :'enc'`, "enc", meta.Encoding)
				add(`LC_COLLATE :'coll'`, "coll", meta.Collate)
				add(`LC_CTYPE :'ctype'`, "ctype", meta.Ctype)
			}
		}
		script := pgPrelude + `exec psql -U "$U" -d postgres -qAtX -v ON_ERROR_STOP=1 ` + strings.Join(vars, " ") + " <<'SQL'\n" + stmt + ";\nSQL"
		return shCmd(script, args...), nil
	case EngineMySQL, EngineMariaDB:
		q := "CREATE DATABASE `" + database + "`"
		if meta.Exists && meta.Charset != "" && meta.Collate != "" {
			q += " CHARACTER SET " + meta.Charset + " COLLATE " + meta.Collate
		}
		return shCmd(mysqlPrelude+`exec "$CLI" -u"$U" -e "$1"`, q), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// importCmd reads a dump on stdin into an existing, empty database. psql stops
// at the first error; the mysql client does so by default.
func importCmd(engine, database string) ([]string, error) {
	if err := validateDBName(database); err != nil {
		return nil, err
	}
	switch engine {
	case EnginePostgres:
		return shCmd(pgPrelude+`exec psql -U "$U" -d "$1" -qX -v ON_ERROR_STOP=1 --single-transaction >/dev/null`, database), nil
	case EngineMySQL, EngineMariaDB:
		return shCmd(mysqlPrelude+`exec "$CLI" -u"$U" --default-character-set=utf8mb4 "$1"`, database), nil
	}
	return nil, fmt.Errorf("unsupported database engine %q", engine)
}

// parseDatabaseList reads listDatabasesCmd's output, dropping system databases.
func parseDatabaseList(engine, out string) []string {
	var dbs []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" || isSystemDatabase(engine, name) {
			continue
		}
		dbs = append(dbs, name)
	}
	return dbs
}

var errNoDatabaseEngine = errors.New("this container does not run a supported database (PostgreSQL, MySQL or MariaDB)")
