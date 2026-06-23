#!/bin/bash
# Migration shortcut — wraps the golang-migrate CLI with DB creds read from .env
# Reads DB_* from server-go/.env and builds DATABASE_URL automatically, so you
# never have to paste the connection string by hand.
#
# Usage:
#   bash /var/www/inventory-app/deploy/migrate.sh                 # apply all pending (up)
#   bash /var/www/inventory-app/deploy/migrate.sh up              # apply all pending
#   bash /var/www/inventory-app/deploy/migrate.sh up 1            # apply next 1 migration
#   bash /var/www/inventory-app/deploy/migrate.sh down 1          # roll back last 1 migration
#   bash /var/www/inventory-app/deploy/migrate.sh version         # print current version
#   bash /var/www/inventory-app/deploy/migrate.sh goto 12         # migrate up or down to version 12
#   bash /var/www/inventory-app/deploy/migrate.sh force 12        # set version to 12 (clears dirty flag, no SQL run)
#   bash /var/www/inventory-app/deploy/migrate.sh create add_foo  # scaffold new up/down migration pair
#   bash /var/www/inventory-app/deploy/migrate.sh drop            # DROP everything (asks for confirmation)

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
APP_DIR="/var/www/inventory-app"
SERVER_GO_DIR="$APP_DIR/server-go"
MIGRATIONS_DIR="$SERVER_GO_DIR/migrations"
ENV_FILE="$SERVER_GO_DIR/.env"

# ── Helpers ───────────────────────────────────────────────────────────────────
log()  { echo "[$(date '+%H:%M:%S')] $*"; }
ok()   { echo "[$(date '+%H:%M:%S')] ✓ $*"; }
fail() { echo "[$(date '+%H:%M:%S')] ✗ $*" >&2; exit 1; }

# ── Read DB credentials from .env ─────────────────────────────────────────────
[ -f "$ENV_FILE" ] || fail ".env not found at $ENV_FILE"

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

# ── Ensure migrate CLI is present ─────────────────────────────────────────────
if ! command -v migrate &>/dev/null; then
  fail "'migrate' CLI not found — install it:
    curl -L https://github.com/golang-migrate/migrate/releases/latest/download/migrate.linux-amd64.tar.gz | tar xz
    sudo mv migrate /usr/local/bin/"
fi

m() { migrate -path "$MIGRATIONS_DIR" -database "$DATABASE_URL" "$@"; }

# ── Dispatch ──────────────────────────────────────────────────────────────────
CMD="${1:-up}"
ARG="${2:-}"

case "$CMD" in
  up)
    log "Applying migrations (up ${ARG:-all})..."
    m up $ARG
    ok "Done"
    m version
    ;;
  down)
    # Default to rolling back a single step rather than everything.
    STEPS="${ARG:-1}"
    log "Rolling back $STEPS migration(s)..."
    m down "$STEPS"
    ok "Done"
    m version
    ;;
  version)
    m version
    ;;
  goto)
    [ -n "$ARG" ] || fail "goto needs a target version, e.g. migrate.sh goto 12"
    log "Migrating to version $ARG..."
    m goto "$ARG"
    ok "Done"
    m version
    ;;
  force)
    [ -n "$ARG" ] || fail "force needs a version, e.g. migrate.sh force 12"
    log "Forcing version to $ARG (no SQL run, clears dirty flag)..."
    m force "$ARG"
    ok "Done"
    m version
    ;;
  create)
    [ -n "$ARG" ] || fail "create needs a name, e.g. migrate.sh create add_foo"
    migrate create -ext sql -dir "$MIGRATIONS_DIR" -seq "$ARG"
    ok "Created migration pair for '$ARG' in $MIGRATIONS_DIR"
    ;;
  drop)
    read -r -p "This DROPS the entire database. Type the DB name ('$DB_NAME') to confirm: " confirm
    [ "$confirm" = "$DB_NAME" ] || fail "Confirmation did not match — aborted"
    log "Dropping everything..."
    m drop -f
    ok "Dropped"
    ;;
  *)
    fail "Unknown command '$CMD'. Valid: up | down | version | goto | force | create | drop"
    ;;
esac
