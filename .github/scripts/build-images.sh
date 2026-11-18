#!/usr/bin/env bash
set -euo pipefail
release=$1
revision=$2
prefix=$3
for service in web auth backend; do
  if docker image inspect "$prefix-$service:$revision" >/dev/null 2>&1; then continue; fi
  case "$service" in
    web) context="$release"; dockerfile="$release/apps/web/Web.Dockerfile";;
    auth) context="$release/apps/auth"; dockerfile="$context/Dockerfile";;
    backend) context="$release/apps/backend"; dockerfile="$context/Dockerfile";;
  esac
  docker build -f "$dockerfile" -t "$prefix-$service:$revision" "$context"
done
