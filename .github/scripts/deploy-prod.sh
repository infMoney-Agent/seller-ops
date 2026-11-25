#!/usr/bin/env bash
set -euo pipefail

# Invoked only as the forced command for the GitHub Actions production deployment key.
command=${SSH_ORIGINAL_COMMAND:-}
if [[ ! $command =~ ^deploy\ ([a-f0-9]{40})$ ]]; then
  echo 'Expected: deploy <commit SHA>' >&2
  exit 2
fi
revision=${BASH_REMATCH[1]}
umask 077
config_dir="$HOME/.config/seller-ops-cd"
# shellcheck source=/dev/null
source "$config_dir/prod.env"
: "${REPO_DIR:?}" "${STATE_DIR:?}" "${COMPOSE_ENV:?}" "${COMPOSE_PROJECT:?}"
HEALTH_HOST=${HEALTH_HOST:-127.0.0.1}
CASDOOR_ADMIN_ADDR=${CASDOOR_ADMIN_ADDR:-127.0.0.1:18000}

mkdir -p "$STATE_DIR/releases"
exec 9>"$STATE_DIR/deploy.lock"
flock -w 60 9

if [[ -n ${DOCKER_START_SCRIPT:-} ]]; then
  bash "$DOCKER_START_SCRIPT"
fi
if [[ -n ${DOCKER_HOST_FILE:-} ]]; then
  export DOCKER_HOST="$(cat "$DOCKER_HOST_FILE")"
fi
docker info --format 'Deployment engine: {{.ServerVersion}}'

git -C "$REPO_DIR" fetch --quiet origin +refs/heads/main:refs/remotes/origin/main
if [[ $(git -C "$REPO_DIR" rev-parse refs/remotes/origin/main) != "$revision" ]]; then
  echo "Skipping superseded main revision $revision"
  exit 0
fi

current="$STATE_DIR/current"
previous=""
if [[ -L "$current" && -f "$current/images.yaml" ]]; then
  previous=$(readlink "$current")
  if [[ "$previous" == "$STATE_DIR/releases/$revision" ]]; then
    echo "Revision $revision is already deployed"
    exit 0
  fi
fi

IFS= read -r registry
IFS= read -r prefix
IFS= read -r reg_user
IFS= read -r reg_pass
test -n "$registry" && test -n "$prefix" && test -n "$reg_user" && test -n "$reg_pass"
if [[ ! $registry =~ ^[a-z0-9][a-z0-9.:-]*$ ]] || [[ ! $prefix =~ ^[a-z0-9][a-z0-9._:/-]*$ ]]; then
  echo 'Invalid image registry or prefix' >&2
  exit 2
fi

export DOCKER_CONFIG="$(mktemp -d "$STATE_DIR/registry.XXXXXX")"
cleanup() {
  case "$DOCKER_CONFIG" in
    "$STATE_DIR"/registry.*) rm -rf -- "$DOCKER_CONFIG" ;;
  esac
}
trap cleanup EXIT
printf '%s\n' "$reg_pass" | docker login "$registry" -u "$reg_user" --password-stdin >/dev/null
unset reg_pass

release="$STATE_DIR/releases/$revision"
rm -rf -- "$release"
mkdir -p "$release"
git -C "$REPO_DIR" archive "$revision" | tar --no-same-owner --same-permissions -xf - -C "$release"

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
  local args=(
    --project-directory "$directory"
    --project-name "$COMPOSE_PROJECT"
    --env-file "$COMPOSE_ENV"
    --profile app
    -f "$directory/compose.yaml"
    -f "$directory/apps/auth/casdoor/compose.prod-http.yaml"
  )
  if [[ -n ${SERVER_OVERRIDE:-} && -f "$SERVER_OVERRIDE" ]]; then
    args+=(-f "$SERVER_OVERRIDE")
  fi
  args+=(-f "$directory/images.yaml")
  docker compose "${args[@]}" "$@"
}

healthy() {
  local directory=$1
  local running
  running=$(compose "$directory" ps --status running --services) || return 1
  for service in proxy casdoor db api web backend ecom-redis; do
    grep -Fxq "$service" <<<"$running" || return 1
  done
  curl -fsS --max-time 4 "http://$HEALTH_HOST/auth/sign-in" -o /dev/null &&
  [[ $(curl -sS --max-time 4 -o /dev/null -w '%{http_code}' "http://$HEALTH_HOST/api/auth/session") == 401 ]] &&
  [[ $(curl -sS --max-time 4 -o /dev/null -w '%{http_code}' "http://$HEALTH_HOST/login") =~ ^(200|302)$ ]] &&
  [[ $(curl -sS --max-time 4 -o /dev/null -w '%{http_code}' "http://$HEALTH_HOST/sellerops.auth.v1.AuthService/GetSession") == 404 ]] &&
  curl -fsS --max-time 4 "http://$CASDOOR_ADMIN_ADDR/.well-known/openid-configuration" -o /dev/null &&
  compose "$directory" exec -T backend seller-backend health
}

wait_healthy() {
  local directory=$1
  for _ in {1..30}; do
    healthy "$directory" && return 0
    sleep 3
  done
  return 1
}

prune_old_releases() {
  local keep_current=$1
  local keep_previous=$2
  for dir in "$STATE_DIR/releases"/*; do
    [[ -d "$dir" ]] || continue
    if [[ "$dir" == "$keep_current" || ( -n "$keep_previous" && "$dir" == "$keep_previous" ) ]]; then
      continue
    fi
    local old_rev
    old_rev=$(basename "$dir")
    for svc in web auth backend; do
      docker image rm -f "$prefix-$svc:$old_rev" >/dev/null 2>&1 || true
    done
    rm -rf -- "$dir"
  done
  docker image prune -f >/dev/null 2>&1 || true
}

compose "$release" config --quiet
compose "$release" pull web api backend backend-migrate
compose "$release" pull --policy missing proxy casdoor db ecom-redis screening-db-init screening-storage-init

if compose "$release" up -d --no-build --pull never --remove-orphans && wait_healthy "$release"; then
  rm -f "$STATE_DIR/current.next"
  ln -s "$release" "$STATE_DIR/current.next"
  mv -Tf "$STATE_DIR/current.next" "$current"
  prune_old_releases "$release" "$previous"
  echo "Deployed main revision $revision"
else
  echo "Deployment failed for $revision" >&2
  if [[ -n "$previous" ]]; then
    echo "Restoring $(basename "$previous")" >&2
    compose "$previous" up -d --no-build --pull never --remove-orphans && wait_healthy "$previous" ||
      echo 'Rollback did not pass health checks' >&2
  fi
  exit 1
fi
