#!/bin/sh
set -eu

read_secret() {
	value="$(cat "$1")"
	if [ -z "$value" ]; then
		echo "database bootstrap secret is empty" >&2
		exit 1
	fi
	printf '%s' "$value"
}

umask 077
export PGPASSWORD="$(read_secret "$POSTGRES_ADMIN_PASSWORD_FILE")"
export AUTH_DB_PASSWORD="$(read_secret "$AUTH_DB_PASSWORD_FILE")"
export DEVICE_DB_PASSWORD="$(read_secret "$DEVICE_DB_PASSWORD_FILE")"
export EVENT_DB_PASSWORD="$(read_secret "$EVENT_DB_PASSWORD_FILE")"
export MEDIA_DB_PASSWORD="$(read_secret "$MEDIA_DB_PASSWORD_FILE")"
export PUSH_DB_PASSWORD="$(read_secret "$PUSH_DB_PASSWORD_FILE")"

attempt=0
until psql -X -v ON_ERROR_STOP=1 -c 'SELECT 1' >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		echo "PostgreSQL did not accept bootstrap connections" >&2
		exit 1
	fi
	sleep 2
done

psql -X -v ON_ERROR_STOP=1 <<'SQL'
\getenv database_name PGDATABASE
\getenv auth_password AUTH_DB_PASSWORD
\getenv device_password DEVICE_DB_PASSWORD
\getenv event_password EVENT_DB_PASSWORD
\getenv media_password MEDIA_DB_PASSWORD
\getenv push_password PUSH_DB_PASSWORD

REVOKE ALL ON DATABASE :"database_name" FROM PUBLIC;

SELECT format('CREATE ROLE auth_service LOGIN PASSWORD %L', :'auth_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'auth_service') \gexec
ALTER ROLE auth_service LOGIN PASSWORD :'auth_password';
CREATE SCHEMA IF NOT EXISTS auth AUTHORIZATION auth_service;
ALTER SCHEMA auth OWNER TO auth_service;
GRANT CONNECT ON DATABASE :"database_name" TO auth_service;

SELECT format('CREATE ROLE device_service LOGIN PASSWORD %L', :'device_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'device_service') \gexec
ALTER ROLE device_service LOGIN PASSWORD :'device_password';
CREATE SCHEMA IF NOT EXISTS device AUTHORIZATION device_service;
ALTER SCHEMA device OWNER TO device_service;
GRANT CONNECT ON DATABASE :"database_name" TO device_service;

SELECT format('CREATE ROLE event_service LOGIN PASSWORD %L', :'event_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'event_service') \gexec
ALTER ROLE event_service LOGIN PASSWORD :'event_password';
CREATE SCHEMA IF NOT EXISTS event AUTHORIZATION event_service;
ALTER SCHEMA event OWNER TO event_service;
GRANT CONNECT ON DATABASE :"database_name" TO event_service;

SELECT format('CREATE ROLE media_service LOGIN PASSWORD %L', :'media_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'media_service') \gexec
ALTER ROLE media_service LOGIN PASSWORD :'media_password';
CREATE SCHEMA IF NOT EXISTS media AUTHORIZATION media_service;
ALTER SCHEMA media OWNER TO media_service;
GRANT CONNECT ON DATABASE :"database_name" TO media_service;

SELECT format('CREATE ROLE push_service LOGIN PASSWORD %L', :'push_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'push_service') \gexec
ALTER ROLE push_service LOGIN PASSWORD :'push_password';
CREATE SCHEMA IF NOT EXISTS push AUTHORIZATION push_service;
ALTER SCHEMA push OWNER TO push_service;
GRANT CONNECT ON DATABASE :"database_name" TO push_service;

REVOKE CREATE ON DATABASE :"database_name" FROM auth_service, device_service, event_service, media_service, push_service;
GRANT CREATE ON DATABASE :"database_name" TO auth_service, device_service, event_service, media_service, push_service;

REVOKE ALL ON SCHEMA public FROM PUBLIC;
SQL

unset PGPASSWORD AUTH_DB_PASSWORD DEVICE_DB_PASSWORD EVENT_DB_PASSWORD MEDIA_DB_PASSWORD PUSH_DB_PASSWORD
echo "database roles and schemas are ready"
