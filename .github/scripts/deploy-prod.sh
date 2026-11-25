#!/usr/bin/env bash
set -euo pipefail

# Invoked only as the forced command for the GitHub Actions production deployment key.
command=${SSH_ORIGINAL_COMMAND:-}
if [[ $command =~ ^deploy\ ([a-f0-9]{40})$ ]]; then
  action=deploy
  revision=${BASH_REMATCH[1]}
elif [[ $command =~ ^upload\ ([a-f0-9]{40})\ (web|auth|backend)\ ([a-f0-9]{64})$ ]]; then
  action=upload
  revision=${BASH_REMATCH[1]}
  service=${BASH_REMATCH[2]}
  archive_digest=${BASH_REMATCH[3]}
else
  echo 'Expected: deploy <commit SHA> or upload <commit SHA> <web|auth|backend> <archive SHA256>' >&2
  exit 2
fi
umask 077
config_dir="$HOME/.config/seller-ops-cd"
# shellcheck source=/dev/null
source "$config_dir/prod.env"
: "${REPO_DIR:?}" "${STATE_DIR:?}" "${COMPOSE_ENV:?}" "${COMPOSE_PROJECT:?}"
HEALTH_HOST=${HEALTH_HOST:-127.0.0.1}
CASDOOR_ADMIN_ADDR=${CASDOOR_ADMIN_ADDR:-127.0.0.1:18000}

mkdir -p "$STATE_DIR/releases"
lock=deploy
[[ $action != upload ]] || lock="upload-$service"
exec 9>"$STATE_DIR/$lock.lock"
flock -w 60 9

if [[ -n ${DOCKER_START_SCRIPT:-} ]]; then
  bash "$DOCKER_START_SCRIPT"
fi
if [[ -n ${DOCKER_HOST_FILE:-} ]]; then
  export DOCKER_HOST="$(cat "$DOCKER_HOST_FILE")"
fi
docker info --format 'Deployment engine: {{.ServerVersion}}'

# Upload jobs run in parallel; serialize updates to the shared Git repository.
exec 8>"$STATE_DIR/git.lock"
flock -w 180 8
fetched=false
for attempt in 1 2 3; do
  if timeout 90 git -c http.version=HTTP/1.1 -C "$REPO_DIR" fetch --quiet --no-tags origin +refs/heads/main:refs/remotes/origin/main; then
    fetched=true
    break
  fi
  [[ $attempt == 3 ]] || sleep 3
done
[[ $fetched == true ]] || exit 1
latest_revision=$(git -C "$REPO_DIR" rev-parse refs/remotes/origin/main)
flock -u 8
if [[ $latest_revision != "$revision" ]]; then
  echo "Skipping superseded main revision $revision"
  # Drain a superseded upload so the sender does not fail with a broken pipe.
  [[ $action != upload ]] || cat >/dev/null
  exit 0
fi

current="$STATE_DIR/current"
previous=""
if [[ $action == deploy && -L "$current" && -f "$current/images.yaml" ]]; then
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
incoming=""
cleanup() {
  case "$incoming" in
    "$STATE_DIR"/upload.*) rm -f -- "$incoming" ;;
  esac
  case "$DOCKER_CONFIG" in
    "$STATE_DIR"/registry.*) rm -rf -- "$DOCKER_CONFIG" ;;
  esac
}
trap cleanup EXIT
printf '%s\n' "$reg_pass" | docker login "$registry" -u "$reg_user" --password-stdin >/dev/null
unset reg_pass

if [[ $action == upload ]]; then
  incoming=$(mktemp "$STATE_DIR/upload.XXXXXX")
  cat >"$incoming"
  actual_digest=$(sha256sum "$incoming")
  if [[ ${actual_digest%% *} != "$archive_digest" ]]; then
    echo 'Image archive checksum mismatch' >&2
    exit 2
  fi
  image="$prefix-$service:$revision"
  python3 - "$incoming" "$image" "$revision" <<'PY'
import json
import sys
import tarfile

with tarfile.open(sys.argv[1], 'r:gz') as archive:
    manifest = json.load(archive.extractfile('manifest.json'))
    if len(manifest) != 1 or manifest[0].get('RepoTags') != [sys.argv[2]]:
        raise SystemExit('Image archive must contain only the expected image tag')
    config = json.load(archive.extractfile(manifest[0]['Config']))
    labels = config.get('config', {}).get('Labels') or {}
    if config.get('os') != 'linux' or config.get('architecture') != 'amd64':
        raise SystemExit('Expected a linux/amd64 image')
    if labels.get('org.opencontainers.image.revision') != sys.argv[3]:
        raise SystemExit('Image archive revision label mismatch')
PY
  gzip -dc "$incoming" | docker load
  image_revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
  if [[ $image_revision != "$revision" ]]; then
    echo 'Image revision label mismatch' >&2
    exit 2
  fi
  docker push "$image"
  docker image tag "$image" "$prefix-$service:main"
  docker push "$prefix-$service:main"
  echo "Published $service image for $revision from production server"
  exit 0
fi

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
