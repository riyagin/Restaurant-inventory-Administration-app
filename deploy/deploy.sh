#!/bin/bash
# Deploy script — pulls latest code, runs migrations, builds the frontend and the
# Go binary, reloads PM2
# Usage:
#   bash /var/www/inventory-app/deploy/deploy.sh          # full deploy
#   bash /var/www/inventory-app/deploy/deploy.sh --no-pull  # skip git pull (local changes)
#   bash /var/www/inventory-app/deploy/deploy.sh --no-migrate  # skip migrations
#   bash /var/www/inventory-app/deploy/deploy.sh --no-client   # skip frontend build (backend-only change)

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
APP_DIR="/var/www/inventory-app"
SERVER_GO_DIR="$APP_DIR/server-go"
CLIENT_DIR="$APP_DIR/client"
PM2_APP="inventory-app"
ENV_FILE="$SERVER_GO_DIR/.env"

# ── Flags ─────────────────────────────────────────────────────────────────────
SKIP_PULL=false
SKIP_MIGRATE=false
SKIP_CLIENT=false
for arg in "$@"; do
  case "$arg" in
    --no-pull)    SKIP_PULL=true ;;
    --no-migrate) SKIP_MIGRATE=true ;;
    --no-client)  SKIP_CLIENT=true ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[$(date '+%H:%M:%S')] $*"; }
ok()   { echo "[$(date '+%H:%M:%S')] ✓ $*"; }
fail() { echo "[$(date '+%H:%M:%S')] ✗ $*" >&2; exit 1; }

cd "$APP_DIR"

# ── 1. Pull latest code ───────────────────────────────────────────────────────
if [ "$SKIP_PULL" = false ]; then
  log "Pulling latest code..."
  git pull --ff-only || fail "git pull failed — resolve conflicts first"
  ok "Code up to date"
else
  log "Skipping git pull"
fi

# ── 2. Read DB credentials from .env ─────────────────────────────────────────
if [ ! -f "$ENV_FILE" ]; then
  fail ".env not found at $ENV_FILE"
fi

# Parse key=value lines, ignore comments and blanks
get_env() { grep -E "^${1}=" "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '"'"'" ; }

DB_HOST="${DB_HOST:-$(get_env DB_HOST)}"
DB_PORT="${DB_PORT:-$(get_env DB_PORT)}"
DB_NAME="${DB_NAME:-$(get_env DB_NAME)}"
DB_USER="${DB_USER:-$(get_env DB_USER)}"
DB_PASSWORD="${DB_PASSWORD:-$(get_env DB_PASSWORD)}"

DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-inventory_app}"
DB_USER="${DB_USER:-postgres}"

[ -z "$DB_PASSWORD" ] && fail "DB_PASSWORD not found in $ENV_FILE"

DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable"

# ── 3. Run migrations ─────────────────────────────────────────────────────────
if [ "$SKIP_MIGRATE" = false ]; then
  log "Running database migrations..."
  if ! command -v migrate &>/dev/null; then
    fail "'migrate' CLI not found — install it:
    curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xz
    sudo mv migrate /usr/local/bin/"
  fi
  migrate -path "$SERVER_GO_DIR/migrations" -database "$DATABASE_URL" up
  ok "Migrations applied"
else
  log "Skipping migrations"
fi

# ── 4. Build React frontend ───────────────────────────────────────────────────
# Runs before the Go build so a failing frontend build aborts the deploy while
# the old binary is still serving — nothing is swapped until both sides compile.
if [ "$SKIP_CLIENT" = false ]; then
  log "Building frontend..."
  command -v npm &>/dev/null || fail "npm not found in PATH"
  cd "$CLIENT_DIR"
  npm ci || npm install
  npm run build || fail "frontend build failed — client/dist left untouched"
  ok "Frontend built: $(du -sh dist | cut -f1)"
else
  log "Skipping frontend build"
fi

# ── 5. Build Go binary ────────────────────────────────────────────────────────
log "Building Go binary..."
if ! command -v go &>/dev/null; then
  fail "Go not found in PATH"
fi
cd "$SERVER_GO_DIR"
go build -o api ./cmd/api
ok "Binary built: $(du -sh api | cut -f1)"

# ── 6. (Re)start PM2 ──────────────────────────────────────────────────────────
# Use delete + start (NOT reload): reload reuses PM2's cached launch options, so
# changes to ecosystem.config.cjs (interpreter, kill_timeout, env) would never
# take effect. delete + start re-reads the config every deploy.
PORT=$(get_env PORT); PORT="${PORT:-5000}"
log "Restarting PM2 process '$PM2_APP' on :$PORT..."

# pm2 delete comes FIRST, before any kill. Killing the port while PM2 still
# manages the app is what *creates* the orphans this block exists to clear:
# autorestart fires, PM2 forks a replacement, and the `pm2 delete` landing a
# moment later drops the app entry without reaping the child it just spawned.
# That child keeps :$PORT, no longer appears in `pm2 list`, and every deploy
# after it crash-loops on 'bind: address already in use'.
pm2 delete "$PM2_APP" 2>/dev/null || true

# port_pids: PIDs listening on $PORT. Uses ss (iproute2, always present) rather
# than fuser (psmisc, often not installed) — the old guard was wrapped in
# `command -v fuser` and silently did nothing on a box without it.
port_pids() {
  ss -lptnH "sport = :${PORT}" 2>/dev/null | grep -oP 'pid=\K[0-9]+' | sort -u
}

# Wait for the port to clear on its own: the server shuts down gracefully in up
# to 5s (kill_timeout is 6000), so the old `sleep 1` returned while the previous
# instance still held the socket even when nothing was orphaned.
for _ in $(seq 1 10); do
  [ -z "$(port_pids)" ] && break
  sleep 1
done

# Anything still holding the port after that is a genuine orphan: SIGTERM for a
# clean shutdown, SIGKILL only if it ignores it.
if [ -n "$(port_pids)" ]; then
  log "Port $PORT still held by PID(s): $(port_pids | tr '\n' ' ')— terminating"
  kill $(port_pids) 2>/dev/null || true
  for _ in $(seq 1 8); do
    [ -z "$(port_pids)" ] && break
    sleep 1
  done
  if [ -n "$(port_pids)" ]; then
    log "Still held — sending SIGKILL"
    kill -9 $(port_pids) 2>/dev/null || true
    sleep 2
  fi
fi

# Refuse to start into a crash loop. Starting anyway is how this went unnoticed
# for hundreds of restarts: PM2 reports success, the binary dies on bind, and
# because it survives the DB connect and migrations first it outlives
# min_uptime — so the restart counter resets and max_restarts never trips.
if [ -n "$(port_pids)" ]; then
  fail "Port $PORT is still in use by PID(s) $(port_pids | tr '\n' ' ')— not starting. Investigate with: ss -lptn 'sport = :$PORT'"
fi

pm2 start "$APP_DIR/deploy/ecosystem.config.cjs"
pm2 save
ok "PM2 started"

# ── 7. Health check ───────────────────────────────────────────────────────────
# Poll rather than sleep once: migrations and the DB connect run before the
# listener binds, so a single 2s check can miss a server that comes up fine.
log "Waiting for server to come up..."
HEALTHY=false
for _ in $(seq 1 15); do
  if curl -sf "http://localhost:${PORT}/api/health" &>/dev/null; then
    HEALTHY=true
    break
  fi
  sleep 1
done

if [ "$HEALTHY" = true ]; then
  ok "Health check passed on :${PORT}"
else
  # A failed health check after a successful build means the deploy did not
  # land, so say so loudly instead of printing "Deploy complete".
  pm2 logs "$PM2_APP" --lines 20 --err --nostream 2>/dev/null || true
  fail "Server not healthy on :${PORT} after 15s — see the error log above"
fi

echo ""
echo "Deploy complete. Useful commands:"
echo "  pm2 logs $PM2_APP       # live logs"
echo "  pm2 status              # process status"
echo "  migrate -path $SERVER_GO_DIR/migrations -database \"\$DATABASE_URL\" version  # current migration version"
