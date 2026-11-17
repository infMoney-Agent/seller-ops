#!/bin/sh
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  --set=app_password="$APP_DB_PASSWORD" --set=auth_password="$AUTH_DB_PASSWORD" <<'SQL'
CREATE ROLE seller_app LOGIN PASSWORD :'app_password';
CREATE ROLE casdoor LOGIN PASSWORD :'auth_password';
CREATE DATABASE seller_ops OWNER seller_app;
CREATE DATABASE casdoor OWNER casdoor;
REVOKE CONNECT ON DATABASE seller_ops FROM PUBLIC;
REVOKE CONNECT ON DATABASE casdoor FROM PUBLIC;
SQL
