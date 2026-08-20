#!/usr/bin/env sh
set -eu
umask 077

SELF="$0"
CONFIG=/etc/vps-monitor/updater.env
PROGRAM=/usr/local/libexec/monitor-updater

usage() {
  echo "usage: $0 install --project-dir /absolute/path --public-key /absolute/path | run" >&2
  exit 2
}

need_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "please run as root" >&2
    exit 1
  fi
}

install_updater() {
  need_root
  shift
  project_dir=""
  public_key=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --project-dir) project_dir="${2:-}"; shift 2 ;;
      --public-key) public_key="${2:-}"; shift 2 ;;
      *) usage ;;
    esac
  done
  case "$project_dir" in /*) ;; *) echo "project directory must be absolute" >&2; exit 2 ;; esac
  case "$public_key" in /*) ;; *) echo "release public key path must be absolute" >&2; exit 2 ;; esac
  [ -f "$project_dir/docker-compose.yml" ] || { echo "docker-compose.yml not found in $project_dir" >&2; exit 1; }
  [ -f "$public_key" ] || { echo "release public key not found: $public_key" >&2; exit 1; }
  command -v openssl >/dev/null 2>&1 || { echo "openssl is required for release signature verification" >&2; exit 1; }
  openssl pkey -pubin -in "$public_key" -noout >/dev/null 2>&1 || { echo "invalid release public key" >&2; exit 1; }
  install -d -m 0755 /etc/vps-monitor /usr/local/libexec
  install -o root -g root -m 0644 "$public_key" /etc/vps-monitor/release-signing.pub
  install -m 0755 "$SELF" "$PROGRAM"
  case "$project_dir" in *"'"*|*"
"*) echo "project directory contains unsupported characters" >&2; exit 2 ;; esac
  {
    printf "PROJECT_DIR='%s'\n" "$project_dir"
    printf "RELEASE_PUBLIC_KEY_FILE='/etc/vps-monitor/release-signing.pub'\n"
  } >"$CONFIG"
  chmod 0600 "$CONFIG"
  cat >/etc/systemd/system/monitor-updater.service <<'EOF'
[Unit]
Description=VPS Monitor signed release updater
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/libexec/monitor-updater run
RuntimeDirectory=monitor-updater
Environment=HOME=/run/monitor-updater
Environment=DOCKER_CONFIG=/run/monitor-updater/docker
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
PrivateTmp=true
ProtectHome=true
EOF
  cat >/etc/systemd/system/monitor-updater.path <<EOF
[Unit]
Description=Watch for VPS Monitor update requests

[Path]
PathExists=$project_dir/data/update-request.json
Unit=monitor-updater.service

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now monitor-updater.path
  echo "monitor updater installed for $project_dir"
}

server_asset_name() {
  machine="$(uname -m)"
  case "$machine" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    armv7l|armv7) arch=armv7 ;;
    i386|i486|i586|i686) arch=386 ;;
    *) echo "unsupported updater architecture: $machine" >&2; return 1 ;;
  esac
  printf 'vps-server-linux-%s\n' "$arch"
}

github_curl() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" "$@"
  else
    curl -fsSL "$@"
  fi
}

write_status() {
  state="$1" version="$2" message="$3" python3 - "$STATUS" <<'PY'
import json, os, sys, tempfile
from datetime import datetime, timezone
path = sys.argv[1]
data = {"state": os.environ["state"], "version": os.environ.get("version", ""), "message": os.environ.get("message", ""), "updated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")}
fd, tmp = tempfile.mkstemp(prefix="update-status-", dir=os.path.dirname(path))
with os.fdopen(fd, "w", encoding="utf-8") as handle:
    json.dump(data, handle, ensure_ascii=True, indent=2)
    handle.write("\n")
os.chmod(tmp, 0o644)
os.replace(tmp, path)
if data["state"] in ("success", "failed"):
    history_path = os.path.join(os.path.dirname(path), "update-history.json")
    try:
        with open(history_path, "r", encoding="utf-8") as handle:
            history = json.load(handle)
        if not isinstance(history, list):
            history = []
    except (OSError, ValueError):
        history = []
    history.append(data)
    history = history[-100:]
    fd, tmp = tempfile.mkstemp(prefix="update-history-", dir=os.path.dirname(path))
    with os.fdopen(fd, "w", encoding="utf-8") as handle:
        json.dump(history, handle, ensure_ascii=True, indent=2)
        handle.write("\n")
    os.chmod(tmp, 0o644)
    os.replace(tmp, history_path)
PY
}

run_updater() {
  need_root
  [ -r "$CONFIG" ] || { echo "updater is not installed" >&2; exit 1; }
  . "$CONFIG"
  case "${PROJECT_DIR:-}" in /*) ;; *) echo "invalid PROJECT_DIR" >&2; exit 1 ;; esac
  [ -f "$PROJECT_DIR/docker-compose.yml" ] || { echo "project directory is invalid" >&2; exit 1; }
  [ -f "$PROJECT_DIR/.env" ] || { echo "project .env is missing" >&2; exit 1; }
  set -a
  . "$PROJECT_DIR/.env"
  set +a
  : "${AUTH_SECRET:?AUTH_SECRET is required}"
  : "${RELEASE_PUBLIC_KEY_FILE:?RELEASE_PUBLIC_KEY_FILE is required}"
  [ -r "$RELEASE_PUBLIC_KEY_FILE" ] || { echo "release public key is not readable" >&2; exit 1; }
  command -v openssl >/dev/null 2>&1 || { echo "openssl is required for release signature verification" >&2; exit 1; }
  UPDATE_REPOSITORY="${UPDATE_REPOSITORY:-ithtelab/yunjing-monitor}"
  [ "$UPDATE_REPOSITORY" = "ithtelab/yunjing-monitor" ] || { echo "unsupported update repository" >&2; exit 1; }
  REQUEST="$PROJECT_DIR/data/update-request.json"
  PROCESSING="$PROJECT_DIR/data/update-request.processing.json"
  STATUS="$PROJECT_DIR/data/update-status.json"
  [ -f "$REQUEST" ] || exit 0
  exec 9>"$PROJECT_DIR/data/update.lock"
  flock -n 9 || exit 0
  mv "$REQUEST" "$PROCESSING"

  version="$(AUTH_SECRET="$AUTH_SECRET" UPDATE_REPOSITORY="$UPDATE_REPOSITORY" python3 - "$PROCESSING" <<'PY'
import hashlib, hmac, json, os, re, sys
from datetime import datetime, timezone
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    req = json.load(handle)
required = ("repository", "version", "created_at", "nonce", "signature")
if any(not isinstance(req.get(key), str) or not req[key] for key in required):
    raise SystemExit("invalid update request")
if req["repository"] != os.environ["UPDATE_REPOSITORY"]:
    raise SystemExit("repository mismatch")
if not re.fullmatch(r"v\d+\.\d+\.\d+", req["version"]):
    raise SystemExit("invalid stable version")
created = datetime.fromisoformat(req["created_at"].replace("Z", "+00:00"))
age = (datetime.now(timezone.utc) - created).total_seconds()
if age < -60 or age > 900:
    raise SystemExit("expired update request")
payload = "\n".join(req[key] for key in ("repository", "version", "created_at", "nonce")).encode()
expected = hmac.new(os.environ["AUTH_SECRET"].encode(), payload, hashlib.sha256).hexdigest()
if not hmac.compare_digest(expected, req["signature"]):
    raise SystemExit("invalid update signature")
print(req["version"])
PY
)" || { write_status failed "" "Update request verification failed"; rm -f "$PROCESSING"; exit 1; }

  binary_name="$(server_asset_name)" || { write_status failed "$version" "Unsupported server architecture"; rm -f "$PROCESSING"; exit 1; }
  binary_target_rel="${SERVER_BINARY:-release/$binary_name}"
  case "$binary_target_rel" in
    "release/$binary_name"|"bin/$binary_name") ;;
    *) write_status failed "$version" "SERVER_BINARY does not match host architecture"; rm -f "$PROCESSING"; exit 1 ;;
  esac
  binary_target="$PROJECT_DIR/$binary_target_rel"
  [ -f "$binary_target" ] || { write_status failed "$version" "Configured server binary is missing"; rm -f "$PROCESSING"; exit 1; }
  tmp="$(mktemp -d)"
  backup="$PROJECT_DIR/.update-backups/$(date +%Y%m%d%H%M%S)-$version"
  cleanup() { rm -rf "$tmp"; }
  trap cleanup EXIT
  write_status checking "$version" "Checking GitHub release metadata"
  github_curl -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" \
    "https://api.github.com/repos/$UPDATE_REPOSITORY/releases/latest" -o "$tmp/release.json"
  python3 - "$tmp/release.json" "$version" "$binary_name" >"$tmp/assets.txt" <<'PY'
import json, re, sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    release = json.load(handle)
if release.get("draft") or release.get("prerelease") or release.get("tag_name") != sys.argv[2]:
    raise SystemExit("requested version is not the latest stable release")
assets = {item.get("name"): item.get("url") for item in release.get("assets", [])}
for name in (sys.argv[3], "SHA256SUMS", "SHA256SUMS.sig"):
    url = assets.get(name)
    if not isinstance(url, str) or not re.fullmatch(r"https://api\.github\.com/repos/[^/]+/[^/]+/releases/assets/\d+", url):
        raise SystemExit("required release asset is missing")
    print(name + "=" + url)
PY
  binary_url="$(sed -n "s/^$binary_name=//p" "$tmp/assets.txt")"
  checksums_url="$(sed -n 's/^SHA256SUMS=//p' "$tmp/assets.txt")"
  signature_url="$(sed -n 's/^SHA256SUMS.sig=//p' "$tmp/assets.txt")"
  write_status downloading "$version" "Downloading signed release assets"
  github_curl -H "Accept: application/octet-stream" "$binary_url" -o "$tmp/$binary_name"
  github_curl -H "Accept: application/octet-stream" "$checksums_url" -o "$tmp/SHA256SUMS"
  github_curl -H "Accept: application/octet-stream" "$signature_url" -o "$tmp/SHA256SUMS.sig"
  if ! openssl dgst -sha256 -verify "$RELEASE_PUBLIC_KEY_FILE" -signature "$tmp/SHA256SUMS.sig" "$tmp/SHA256SUMS" >/dev/null 2>&1; then
    write_status failed "$version" "Release signature verification failed"
    rm -f "$PROCESSING"
    exit 1
  fi
  expected="$(awk -v name="$binary_name" '$2==name {print tolower($1)}' "$tmp/SHA256SUMS")"
  actual="$(sha256sum "$tmp/$binary_name" | awk '{print tolower($1)}')"
  [ -n "$expected" ] && [ "$expected" = "$actual" ] || { write_status failed "$version" "SHA-256 verification failed"; rm -f "$PROCESSING"; exit 1; }
  chmod 0755 "$tmp/$binary_name"

  mkdir -p "$backup"
  cp -p "$binary_target" "$backup/vps-server"
  write_status installing "$version" "Backing up data and rebuilding container"
  cd "$PROJECT_DIR"
  docker compose stop monitor-party >/dev/null
  tar -czf "$backup/data.tar.gz" data
  cp "$tmp/$binary_name" "$binary_target"
  rollback() {
    reason="${1:-Health check failed; previous application version restored}"
    cp "$backup/vps-server" "$binary_target"
    docker compose build --no-cache monitor-party >/dev/null 2>&1 || true
    docker compose up -d --force-recreate monitor-party >/dev/null 2>&1 || true
    write_status failed "$version" "$reason"
    rm -f "$PROCESSING"
  }
  if ! docker compose build --no-cache monitor-party; then
    rollback "Container image build failed; previous application version restored"
    exit 1
  fi
  if ! docker compose up -d --force-recreate monitor-party; then
    rollback "Container start failed; previous application version restored"
    exit 1
  fi
  healthy=""
  for _ in $(seq 1 45); do
    healthy="$(docker inspect --format '{{.State.Health.Status}}' monitor-party 2>/dev/null || true)"
    [ "$healthy" = healthy ] && break
    sleep 2
  done
  if [ "$healthy" != healthy ]; then
    rollback "Health check failed; previous application version restored"
    exit 1
  fi
  write_status success "$version" "Update completed successfully"
  rm -f "$PROCESSING"
  find "$PROJECT_DIR/.update-backups" -mindepth 1 -maxdepth 1 -type d -mtime +30 -exec rm -rf {} + 2>/dev/null || true
}

case "${1:-}" in
  install) install_updater "$@" ;;
  run) run_updater ;;
  *) usage ;;
esac
