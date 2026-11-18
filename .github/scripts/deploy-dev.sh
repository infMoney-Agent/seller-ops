#!/usr/bin/env bash
set -euo pipefail

# Invoked only as the forced command for the GitHub Actions deployment key.
command=${SSH_ORIGINAL_COMMAND:-}
if [[ ! $command =~ ^deploy\ ([a-f0-9]{40})$ ]]; then
  echo 'Expected: deploy <commit SHA>' >&2
  exit 2
fi
revision=${BASH_REMATCH[1]}
umask 077
config_dir="$HOME/.config/seller-ops-cd"
source "$config_dir/dev.env"
: "${REPO_DIR:?}" "${STATE_DIR:?}" "${COMPOSE_ENV:?}" "${COMPOSE_PROJECT:?}"
: "${SERVER_OVERRIDE:?}" "${DOCKER_HOST_FILE:?}" "${HEALTH_ADDRESS:?}"
mkdir -p "$STATE_DIR/releases"
exec 9>"$STATE_DIR/deploy.lock"
flock -w 60 9

if [[ -n ${DOCKER_START_SCRIPT:-} ]]; then
  bash "$DOCKER_START_SCRIPT"
fi
export DOCKER_HOST="$(cat "$DOCKER_HOST_FILE")"
docker info --format 'Deployment engine: {{.ServerVersion}}'

git -C "$REPO_DIR" fetch --quiet origin +refs/heads/dev:refs/remotes/origin/dev
if [[ $(git -C "$REPO_DIR" rev-parse refs/remotes/origin/dev) != "$revision" ]]; then
  echo "Skipping superseded dev revision $revision"
  exit 0
fi

current="$STATE_DIR/current"
test -L "$current" && test -f "$current/images.yaml"
release="$STATE_DIR/releases/$revision"
if [[ $(readlink "$current") == "$release" ]]; then
  echo "Revision $revision is already deployed"
  exit 0
fi

IFS= read -r actor
IFS= read -r token
test -n "$actor" && test -n "$token"
if [[ -z ${BUILD_IMAGE_SCRIPT:-} ]]; then
  export DOCKER_CONFIG="$(mktemp -d "$STATE_DIR/registry.XXXXXX")"
  cleanup() {
    case "$DOCKER_CONFIG" in
      "$STATE_DIR"/registry.*) rm -r -- "$DOCKER_CONFIG" ;;
    esac
  }
  trap cleanup EXIT
  printf '%s\n' "$token" | docker login ghcr.io -u "$actor" --password-stdin >/dev/null
fi
unset token

mkdir -p "$release"
git -C "$REPO_DIR" archive "$revision" | tar -xf - -C "$release"
prefix=ghcr.io/infmoney-agent/seller-ops
if [[ -n ${BUILD_IMAGE_SCRIPT:-} ]]; then
  prefix=seller-ops-dev
  "$BUILD_IMAGE_SCRIPT" "$release" "$revision" "$prefix"
fi
cat >"$release/images.yaml" <<YAML
services:
  web:
    image: $prefix-web:$revision
  api:
    image: $prefix-auth:$revision
  backend:
    image: $prefix-backend:$revision
  backend-migrate:
    image: $prefix-backend:$revision
YAML

compose() {
  local directory=$1
  shift
  docker compose --project-directory "$directory" --project-name "$COMPOSE_PROJECT" \
    --env-file "$COMPOSE_ENV" --profile app \
    -f "$directory/compose.yaml" -f "$directory/apps/auth/casdoor/compose.local.yaml" \
    -f "$SERVER_OVERRIDE" -f "$directory/images.yaml" "$@"
}

healthy() {
  local directory=$1
  local running
  running=$(compose "$directory" ps --status running --services) || return 1
  local business_services
  if compose "$directory" config --services | grep -Fxq backend; then
    business_services="backend ecom-redis"
  else
    business_services="ecom-api ecom-redis ecom-worker ecom-beat"
  fi
  for service in proxy casdoor db api web $business_services; do
    grep -Fxq "$service" <<<"$running" || return 1
  done
  curl -fsS --max-time 4 -H 'Host: localhost:18443' \
    "http://$HEALTH_ADDRESS:18443/auth/sign-in" -o /dev/null &&
  [[ $(curl -sS --max-time 4 -o /dev/null -w '%{http_code}' \
    -H 'Host: localhost:18443' "http://$HEALTH_ADDRESS:18443/api/auth/session") == 401 ]] &&
  curl -fsS --max-time 4 -H 'Host: localhost:18444' \
    "http://$HEALTH_ADDRESS:18444/.well-known/openid-configuration" -o /dev/null || return 1
  if compose "$directory" config --services | grep -Fxq backend; then
    compose "$directory" exec -T backend seller-backend health
  else
    compose "$directory" exec -T web node -e "fetch('http://ecom-api:8000/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
  fi
}

wait_healthy() {
  local directory=$1
  for attempt in {1..25}; do
    healthy "$directory" && return 0
    sleep 3
  done
  return 1
}

previous=$(readlink "$current")
compose "$release" config --quiet
if [[ -z ${BUILD_IMAGE_SCRIPT:-} ]]; then
  compose "$release" pull web api backend backend-migrate screening-db-init screening-storage-init
else
  # App images are built locally; newly introduced one-off base images still need provisioning.
  compose "$release" pull --policy missing screening-db-init screening-storage-init
fi
if compose "$release" up -d --no-build --pull never --remove-orphans && wait_healthy "$release"; then
  rm -f "$STATE_DIR/current.next"
  ln -s "$release" "$STATE_DIR/current.next"
  mv -Tf "$STATE_DIR/current.next" "$current"
  echo "Deployed $revision"
else
  echo "Deployment failed, restoring $(basename "$previous")" >&2
  compose "$previous" up -d --no-build --pull never --remove-orphans && wait_healthy "$previous" ||
    echo 'Rollback did not pass health checks' >&2
  exit 1
fi
