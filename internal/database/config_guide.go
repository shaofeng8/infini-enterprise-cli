package database

import "strings"

// TypeGuide is the structured --config catalog for one driver. Agents should
// read this (db types, or db ls --type) rather than inferring host/port from
// another driver: keys are prefixed (dm_host, not host).
type TypeGuide struct {
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	CanAdd      bool     `json:"canAdd"`
	CanTest     bool     `json:"canTest"`
	DefaultPort int      `json:"defaultPort,omitempty"`
	Required    []string `json:"required"`
	Optional    []string `json:"optional,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Example     string   `json:"example"`
}

var typeGuides = []TypeGuide{
	{
		Type: "mysql", Title: "MySQL", CanAdd: true, CanTest: true, DefaultPort: 3306,
		Required: []string{"mysql_host", "mysql_port", "mysql_username", "mysql_password", "mysql_database"},
		Optional: []string{"deep_optimization"},
		Notes:    "deep_optimization defaults to true in the UI.",
		Example:  `{"mysql_host":"127.0.0.1","mysql_port":3306,"mysql_username":"root","mysql_password":"...","mysql_database":"sales","deep_optimization":true}`,
	},
	{
		Type: "postgres", Title: "PostgreSQL", CanAdd: true, CanTest: true, DefaultPort: 5432,
		Required: []string{"pg_host", "pg_port", "pg_username", "pg_password", "pg_database"},
		Optional: []string{"pg_schema", "deep_optimization"},
		Notes:    "supabase uses the same keys.",
		Example:  `{"pg_host":"127.0.0.1","pg_port":5432,"pg_username":"postgres","pg_password":"...","pg_database":"app","pg_schema":"public"}`,
	},
	{
		Type: "supabase", Title: "Supabase (Postgres)", CanAdd: true, CanTest: true, DefaultPort: 5432,
		Required: []string{"pg_host", "pg_port", "pg_username", "pg_password", "pg_database"},
		Optional: []string{"pg_schema", "deep_optimization"},
		Example:  `{"pg_host":"127.0.0.1","pg_port":5432,"pg_username":"postgres","pg_password":"...","pg_database":"app","pg_schema":"public"}`,
	},
	{
		Type: "gbase8a", Title: "GBase 8a", CanAdd: true, CanTest: true, DefaultPort: 5258,
		Required: []string{"gbase_host", "gbase_port", "gbase_username", "gbase_password", "gbase_database"},
		Optional: []string{"deep_optimization"},
		Notes:    "Legacy type alias gbase is normalized to gbase8a.",
		Example:  `{"gbase_host":"127.0.0.1","gbase_port":5258,"gbase_username":"...","gbase_password":"...","gbase_database":"gbase"}`,
	},
	{
		Type: "clickhouse", Title: "ClickHouse", CanAdd: true, CanTest: true, DefaultPort: 8123,
		Required: []string{"clickhouse_host", "clickhouse_port", "clickhouse_username", "clickhouse_password", "clickhouse_database"},
		Optional: []string{"clickhouse_connector_v2", "deep_optimization"},
		Notes:    "Port is the HTTP port. clickhouse_connector_v2 defaults to true in the UI.",
		Example:  `{"clickhouse_host":"127.0.0.1","clickhouse_port":8123,"clickhouse_username":"default","clickhouse_password":"...","clickhouse_database":"default","clickhouse_connector_v2":true}`,
	},
	{
		Type: "dm", Title: "Dameng / 达梦", CanAdd: true, CanTest: true, DefaultPort: 5236,
		Required: []string{"dm_host", "dm_port", "dm_username", "dm_password", "dm_database"},
		Optional: []string{"deep_optimization"},
		Notes:    "Do not use host/port/username. Keys are dm_* only.",
		Example:  `{"dm_host":"127.0.0.1","dm_port":5236,"dm_username":"SYSDBA","dm_password":"...","dm_database":"DAMENG","deep_optimization":true}`,
	},
	{
		Type: "doris", Title: "Apache Doris", CanAdd: true, CanTest: true, DefaultPort: 9030,
		Required: []string{"doris_host", "doris_port", "doris_username", "doris_password", "doris_database"},
		Optional: []string{"deep_optimization"},
		Example:  `{"doris_host":"127.0.0.1","doris_port":9030,"doris_username":"root","doris_password":"...","doris_database":"sales"}`,
	},
	{
		Type: "starrocks", Title: "StarRocks", CanAdd: true, CanTest: true, DefaultPort: 9030,
		Required: []string{"starrocks_host", "starrocks_port", "starrocks_username", "starrocks_password", "starrocks_database"},
		Optional: []string{"deep_optimization"},
		Example:  `{"starrocks_host":"127.0.0.1","starrocks_port":9030,"starrocks_username":"root","starrocks_password":"...","starrocks_database":"sales"}`,
	},
	{
		Type: "kingbase", Title: "KingBase / 人大金仓", CanAdd: true, CanTest: true, DefaultPort: 54321,
		Required: []string{"kingbase_host", "kingbase_port", "kingbase_username", "kingbase_password", "kingbase_database"},
		Optional: []string{"kingbase_schema", "deep_optimization"},
		Example:  `{"kingbase_host":"127.0.0.1","kingbase_port":54321,"kingbase_username":"...","kingbase_password":"...","kingbase_database":"app","kingbase_schema":"public"}`,
	},
	{
		Type: "sqlserver", Title: "SQL Server", CanAdd: true, CanTest: true, DefaultPort: 1433,
		Required: []string{"sqlserver_host", "sqlserver_port", "sqlserver_username", "sqlserver_password", "sqlserver_database"},
		Optional: []string{"sqlserver_schema", "deep_optimization"},
		Example:  `{"sqlserver_host":"127.0.0.1","sqlserver_port":1433,"sqlserver_username":"sa","sqlserver_password":"...","sqlserver_database":"sales","sqlserver_schema":"dbo"}`,
	},
	{
		Type: "oracle", Title: "Oracle", CanAdd: true, CanTest: true, DefaultPort: 1521,
		Required: []string{"oracle_host", "oracle_port", "oracle_username", "oracle_password", "oracle_database"},
		Optional: []string{"oracle_schema", "deep_optimization"},
		Notes:    "oracle_database is the service name / SID.",
		Example:  `{"oracle_host":"127.0.0.1","oracle_port":1521,"oracle_username":"...","oracle_password":"...","oracle_database":"ORCL","oracle_schema":"APP"}`,
	},
	{
		Type: "snowflake", Title: "Snowflake", CanAdd: true, CanTest: true,
		Required: []string{"snowflake_host", "snowflake_username", "snowflake_password", "snowflake_database"},
		Optional: []string{"snowflake_schema", "deep_optimization"},
		Notes:    "No port field; snowflake_host is the account URL.",
		Example:  `{"snowflake_host":"xy12345.aws.snowflakecomputing.com","snowflake_username":"...","snowflake_password":"...","snowflake_database":"ANALYTICS","snowflake_schema":"PUBLIC"}`,
	},
	{
		Type: "sqlite", Title: "SQLite", CanAdd: true, CanTest: true,
		Required: []string{"sqlite_path"},
		Optional: []string{"deep_optimization"},
		Notes:    "The key is sqlite_path, not path. The server copies the file into its store.",
		Example:  `{"sqlite_path":"/data/chinook.sqlite"}`,
	},
	{
		Type: "duckdb", Title: "DuckDB", CanAdd: true, CanTest: true,
		Required: []string{"duckdb_path"},
		Optional: []string{"duckdb_schema", "duckdb_read_only", "deep_optimization"},
		Example:  `{"duckdb_path":"/data/warehouse.duckdb","duckdb_read_only":true}`,
	},
	{
		Type: "file", Title: "File / object store", CanAdd: true, CanTest: true,
		Required: []string{"file_system", "directory"},
		Optional: []string{"endpoint", "access_key_id", "access_key_secret"},
		Notes:    "file_system is file, oss, s3 or cos. Cloud also needs endpoint and access keys. directory for cloud is a URI (oss://, s3a://, cosn://).",
		Example:  `{"file_system":"file","directory":"datasets/orders"}`,
	},
	{
		Type: "deltalake", Title: "Delta Lake", CanAdd: true, CanTest: true,
		Required: []string{"file_system", "delta_path", "delta_database"},
		Optional: []string{"endpoint", "access_key_id", "access_key_secret"},
		Example:  `{"file_system":"file","delta_path":"/data/delta/sales","delta_database":"sales"}`,
	},
	{
		Type: "mongodb", Title: "MongoDB", CanAdd: false, CanTest: true, DefaultPort: 27017,
		Required: []string{"mongodb_host", "mongodb_port", "mongodb_database"},
		Optional: []string{"mongodb_username", "mongodb_password", "mongodb_auth_database", "deep_optimization"},
		Notes:    "db test accepts this type; db add may reject it depending on the deployment.",
		Example:  `{"mongodb_host":"127.0.0.1","mongodb_port":27017,"mongodb_database":"app"}`,
	},
	{
		Type: "elasticsearch", Title: "Elasticsearch", CanAdd: false, CanTest: true, DefaultPort: 9200,
		Required: []string{"elasticsearch_host", "elasticsearch_port", "elasticsearch_auth_mode"},
		Optional: []string{"elasticsearch_username", "elasticsearch_password", "elasticsearch_api_key", "elasticsearch_ssl", "deep_optimization"},
		Notes:    "elasticsearch_auth_mode is basic or apikey. basic needs username+password; apikey needs elasticsearch_api_key.",
		Example:  `{"elasticsearch_host":"127.0.0.1","elasticsearch_port":9200,"elasticsearch_auth_mode":"basic","elasticsearch_username":"elastic","elasticsearch_password":"...","elasticsearch_ssl":false}`,
	},
}

// TypeGuides returns every driver catalog entry.
func TypeGuides() []TypeGuide {
	out := make([]TypeGuide, len(typeGuides))
	copy(out, typeGuides)
	return out
}

// GuideFor returns the catalog for one driver. gbase is accepted as gbase8a.
func GuideFor(dbType string) (TypeGuide, bool) {
	normalized := strings.ToLower(strings.TrimSpace(dbType))
	if normalized == "gbase" {
		normalized = "gbase8a"
	}
	for _, guide := range typeGuides {
		if guide.Type == normalized {
			return guide, true
		}
	}
	return TypeGuide{}, false
}

// ConfigExample returns a compact example for one type, or empty if unknown.
func ConfigExample(dbType string) string {
	guide, ok := GuideFor(dbType)
	if !ok {
		return ""
	}
	return guide.Example
}

// ConfigGuide is the connection-config catalog as prose, for --help and spec.
const ConfigGuide = `Connection config (--config) is a JSON object. Field names are driver-prefixed
(dm_host, not host). The server stores the object as a string; the CLI does not
rename keys. Pass JSON inline, @file, or @-.

Do not infer keys from another driver, and do not use db ls to discover them:
ls only lists saved sources. Run:

  infini-cli db types
  infini-cli db types dm

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
