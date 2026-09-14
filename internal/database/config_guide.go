package database

import "strings"

// ConfigGuide is the connection-config catalog for `db add` / `db test`.
//
// Field names are the ones the UI submits and DatabaseHub reads. They are
// prefixed by driver (`dm_host`, not `host`) and the whole object is a JSON
// string on the wire. An agent that invents un-prefixed keys will save a
// record the inspector cannot open.
const ConfigGuide = `Connection config (--config) is a JSON object. Field names are driver-prefixed
(dm_host, not host). The server stores the object as a string; the CLI does not
rename keys. Pass JSON inline, @file, or @-.

Shared flags for db add / db update:

  --name           unique name used in SQL (required on add)
  --type           driver, one of the types below (required on add)
  --config         JSON object described per type (required on add)
  --nickname       display name
  --description    free text
  --disabled       create disabled (add only)

Always db test --type <type> --config @file.json before db add.

Passwords belong in --config JSON, not as flags. Object-storage secrets for
file/deltalake cloud backends go in the config too (access_key_secret).

================================================================================
JDBC-style types
================================================================================

deep_optimization (boolean, default true on mysql, optional elsewhere) is
accepted on every JDBC-style type below.

mysql  (default port 3306)
  required: mysql_host, mysql_port, mysql_username, mysql_password, mysql_database
  optional: deep_optimization
  example: {"mysql_host":"127.0.0.1","mysql_port":3306,"mysql_username":"root","mysql_password":"...","mysql_database":"sales","deep_optimization":true}

postgres / supabase  (default port 5432; supabase uses the same keys)
  required: pg_host, pg_port, pg_username, pg_password, pg_database
  optional: pg_schema, deep_optimization
  example: {"pg_host":"127.0.0.1","pg_port":5432,"pg_username":"postgres","pg_password":"...","pg_database":"app","pg_schema":"public"}

gbase8a  (GBase 8a; legacy alias "gbase" is normalized to gbase8a; default port 5258)
  required: gbase_host, gbase_port, gbase_username, gbase_password, gbase_database
  optional: deep_optimization

clickhouse  (HTTP port, default 8123)
  required: clickhouse_host, clickhouse_port, clickhouse_username, clickhouse_password, clickhouse_database
  optional: clickhouse_connector_v2 (boolean, default true), deep_optimization

dm  (Dameng / 达梦, default port 5236)
  required: dm_host, dm_port, dm_username, dm_password, dm_database
  optional: deep_optimization
  example: {"dm_host":"127.0.0.1","dm_port":5236,"dm_username":"SYSDBA","dm_password":"...","dm_database":"DAMENG","deep_optimization":true}

doris  (MySQL protocol, default port 9030)
  required: doris_host, doris_port, doris_username, doris_password, doris_database
  optional: deep_optimization

starrocks  (MySQL protocol, default port 9030)
  required: starrocks_host, starrocks_port, starrocks_username, starrocks_password, starrocks_database
  optional: deep_optimization

kingbase  (人大金仓, default port 54321)
  required: kingbase_host, kingbase_port, kingbase_username, kingbase_password, kingbase_database
  optional: kingbase_schema, deep_optimization

sqlserver  (default port 1433)
  required: sqlserver_host, sqlserver_port, sqlserver_username, sqlserver_password, sqlserver_database
  optional: sqlserver_schema, deep_optimization

oracle  (default port 1521)
  required: oracle_host, oracle_port, oracle_username, oracle_password, oracle_database
  optional: oracle_schema, deep_optimization
  oracle_database is the service name / SID.

snowflake  (no port field; host is the account URL)
  required: snowflake_host, snowflake_username, snowflake_password, snowflake_database
  optional: snowflake_schema, deep_optimization
  example: {"snowflake_host":"xy12345.ap-northeast-1.aws.snowflakecomputing.com","snowflake_username":"...","snowflake_password":"...","snowflake_database":"ANALYTICS","snowflake_schema":"PUBLIC"}

================================================================================
Embedded files
================================================================================

sqlite
  required: sqlite_path   (NOT "path")
  optional: deep_optimization
  example: {"sqlite_path":"/data/chinook.sqlite"}
  The server copies the file into its own store; a later db upload replaces it.

duckdb
  required: duckdb_path
  optional: duckdb_schema, duckdb_read_only (boolean, default false), deep_optimization
  example: {"duckdb_path":"/data/warehouse.duckdb","duckdb_read_only":true}

================================================================================
Object / file stores
================================================================================

file_system is one of: file, oss, s3, cos.
When file_system is "file", only the local path is used.
When it is oss, s3 or cos, also send endpoint, access_key_id, access_key_secret.
directory / delta_path for cloud is a URI (oss://bucket/prefix, s3a://bucket/prefix, cosn://bucket/prefix).

file
  required: file_system, directory
  optional (cloud): endpoint, access_key_id, access_key_secret
  local example: {"file_system":"file","directory":"datasets/orders"}
  oss example: {"file_system":"oss","directory":"oss://bucket/orders","endpoint":"oss-cn-hangzhou.aliyuncs.com","access_key_id":"...","access_key_secret":"..."}

deltalake
  required: file_system, delta_path, delta_database
  optional (cloud): endpoint, access_key_id, access_key_secret
  example: {"file_system":"file","delta_path":"/data/delta/sales","delta_database":"sales"}

================================================================================
Document stores (testable; not in db add --type enum on some deployments)
================================================================================

The add DTO may reject these even if db test accepts them. Prefer db test first.

mongodb  (default port 27017)
  required: mongodb_host, mongodb_port, mongodb_database
  optional: mongodb_username, mongodb_password, mongodb_auth_database, deep_optimization

elasticsearch  (default port 9200)
  required: elasticsearch_host, elasticsearch_port, elasticsearch_auth_mode (basic|apikey)
  optional: elasticsearch_ssl (boolean)
  basic also needs: elasticsearch_username, elasticsearch_password
  apikey also needs: elasticsearch_api_key
`

// ConfigExample returns a compact example for one type, or empty if unknown.
func ConfigExample(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "mysql":
		return `{"mysql_host":"127.0.0.1","mysql_port":3306,"mysql_username":"root","mysql_password":"...","mysql_database":"sales","deep_optimization":true}`
	case "postgres", "supabase":
		return `{"pg_host":"127.0.0.1","pg_port":5432,"pg_username":"postgres","pg_password":"...","pg_database":"app","pg_schema":"public"}`
	case "gbase8a", "gbase":
		return `{"gbase_host":"127.0.0.1","gbase_port":5258,"gbase_username":"...","gbase_password":"...","gbase_database":"gbase"}`
	case "clickhouse":
		return `{"clickhouse_host":"127.0.0.1","clickhouse_port":8123,"clickhouse_username":"default","clickhouse_password":"...","clickhouse_database":"default","clickhouse_connector_v2":true}`
	case "dm":
		return `{"dm_host":"127.0.0.1","dm_port":5236,"dm_username":"SYSDBA","dm_password":"...","dm_database":"DAMENG","deep_optimization":true}`
	case "doris":
		return `{"doris_host":"127.0.0.1","doris_port":9030,"doris_username":"root","doris_password":"...","doris_database":"sales"}`
	case "starrocks":
		return `{"starrocks_host":"127.0.0.1","starrocks_port":9030,"starrocks_username":"root","starrocks_password":"...","starrocks_database":"sales"}`
	case "kingbase":
		return `{"kingbase_host":"127.0.0.1","kingbase_port":54321,"kingbase_username":"...","kingbase_password":"...","kingbase_database":"app","kingbase_schema":"public"}`
	case "sqlserver":
		return `{"sqlserver_host":"127.0.0.1","sqlserver_port":1433,"sqlserver_username":"sa","sqlserver_password":"...","sqlserver_database":"sales","sqlserver_schema":"dbo"}`
	case "oracle":
		return `{"oracle_host":"127.0.0.1","oracle_port":1521,"oracle_username":"...","oracle_password":"...","oracle_database":"ORCL","oracle_schema":"APP"}`
	case "snowflake":
		return `{"snowflake_host":"xy12345.aws.snowflakecomputing.com","snowflake_username":"...","snowflake_password":"...","snowflake_database":"ANALYTICS","snowflake_schema":"PUBLIC"}`
	case "sqlite":
		return `{"sqlite_path":"/data/chinook.sqlite"}`
	case "duckdb":
		return `{"duckdb_path":"/data/warehouse.duckdb","duckdb_read_only":true}`
	case "file":
		return `{"file_system":"file","directory":"datasets/orders"}`
	case "deltalake":
		return `{"file_system":"file","delta_path":"/data/delta/sales","delta_database":"sales"}`
	case "mongodb":
		return `{"mongodb_host":"127.0.0.1","mongodb_port":27017,"mongodb_database":"app"}`
	case "elasticsearch":
		return `{"elasticsearch_host":"127.0.0.1","elasticsearch_port":9200,"elasticsearch_auth_mode":"basic","elasticsearch_username":"elastic","elasticsearch_password":"...","elasticsearch_ssl":false}`
	default:
		return ""
	}
}
