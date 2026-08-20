#!/bin/sh
set -eu

# Container starts as root only long enough to fix the mounted data dir,
# then drops to UID 10001 (monitor). Baota/www uploads often leave data/
# owned by root or www, which causes:
#   open /app/data/server.json: permission denied

DATA_DIR="$(dirname "${DATA_PATH:-/app/data/server.json}")"
mkdir -p "$DATA_DIR"

# Fail closed if the non-root process cannot create/update JSON stores. Never
# make credentials, sessions, and node data world-writable as a workaround.
if [ "$(id -u)" = "0" ]; then
  if ! chown -R monitor:monitor "$DATA_DIR"; then
    echo "fatal: cannot assign $DATA_DIR to UID/GID 10001; refusing insecure permission fallback" >&2
    exit 1
  fi
  if ! chmod -R u+rwX,g+rwX,o-rwx "$DATA_DIR"; then
    echo "fatal: cannot secure permissions on $DATA_DIR" >&2
    exit 1
  fi
  if ! su-exec monitor:monitor sh -c "touch '$DATA_DIR/.write_test' && rm -f '$DATA_DIR/.write_test'" 2>/dev/null; then
    echo "fatal: $DATA_DIR is not writable by UID/GID 10001" >&2
    exit 1
  fi
  exec su-exec monitor:monitor /app/vps-server "$@"
fi

exec /app/vps-server "$@"
