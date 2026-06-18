#!/bin/bash
# Deploy script — pulls latest code, runs migrations, builds Go binary, reloads PM2
# Usage:
#   bash /var/www/inventory-app/deploy/deploy.sh          # full deploy
#   bash /var/www/inventory-app/deploy/deploy.sh --no-pull  # skip git pull (local changes)
#   bash /var/www/inventory-app/deploy/deploy.sh --no-migrate  # skip migrations

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
APP_DIR="/var/www/inventory-app"
SERVER_GO_DIR="$APP_DIR/server-go"
PM2_APP="inventory-app"
ENV_FILE="$SERVER_GO_DIR/.env"

# ── Flags ─────────────────────────────────────────────────────────────────────
SKIP_PULL=false
SKIP_MIGRATE=false
for arg in "$@"; do
  case "$arg" in
    --no-pull)    SKIP_PULL=true ;;
    --no-migrate) SKIP_MIGRATE=true ;;
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

# ── 4. Build Go binary ────────────────────────────────────────────────────────
log "Building Go binary..."
if ! command -v go &>/dev/null; then
  fail "Go not found in PATH"
fi
cd "$SERVER_GO_DIR"
go build -o api ./cmd/api
ok "Binary built: $(du -sh api | cut -f1)"

# ── 5. Reload PM2 ─────────────────────────────────────────────────────────────
log "Reloading PM2 process '$PM2_APP'..."
if pm2 describe "$PM2_APP" &>/dev/null; then
  pm2 reload "$PM2_APP"
  ok "PM2 reloaded"
else
  log "Process not found — starting fresh from ecosystem config..."
  pm2 start "$APP_DIR/deploy/ecosystem.config.cjs"
  pm2 save
  ok "PM2 started"
fi

# ── 6. Health check ───────────────────────────────────────────────────────────
log "Waiting for server to come up..."
sleep 2
PORT=$(get_env PORT)
PORT="${PORT:-5000}"
if curl -sf "http://localhost:${PORT}/api/health" &>/dev/null; then
  ok "Health check passed on :${PORT}"
else
  log "Health endpoint not responding (may not exist) — check logs with: pm2 logs $PM2_APP"
fi

echo ""
echo "Deploy complete. Useful commands:"
echo "  pm2 logs $PM2_APP       # live logs"
echo "  pm2 status              # process status"
echo "  migrate -path $SERVER_GO_DIR/migrations -database \"\$DATABASE_URL\" version  # current migration version"
