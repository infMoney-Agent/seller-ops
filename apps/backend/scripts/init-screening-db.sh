#!/bin/sh
set -eu
: "${SCREENING_DB_PASSWORD:?Set SCREENING_DB_PASSWORD}"
export PGPASSWORD="${POSTGRES_PASSWORD:?Set POSTGRES_PASSWORD}"
psql -h db -U postgres -d postgres -v ON_ERROR_STOP=1 \
  --set=screening_password="$SCREENING_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE seller_screening LOGIN PASSWORD %L', :'screening_password')
 WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname='seller_screening') \gexec
SELECT 'CREATE DATABASE seller_screening OWNER seller_screening'
 WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname='seller_screening') \gexec
REVOKE CONNECT ON DATABASE seller_screening FROM PUBLIC;
GRANT CONNECT ON DATABASE seller_screening TO seller_screening;
SQL
