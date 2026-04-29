#!/bin/bash
# Database + uploads backup script
# Recommended: run daily via cron
#   0 2 * * * /var/www/inventory-app/deploy/backup.sh >> /var/log/inventory-app/backup.log 2>&1

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
DB_NAME="inventory_app"
DB_USER="postgres"
DB_PASSWORD="CHANGE_THIS_TO_YOUR_DB_PASSWORD"
BACKUP_DIR="/var/backups/inventory-app"
UPLOADS_DIR="/var/www/inventory-app/server/uploads"
KEEP_DAYS=30   # delete backups older than this

# ── Setup ─────────────────────────────────────────────────────────────────────
DATE=$(date +"%Y-%m-%d_%H-%M")
mkdir -p "$BACKUP_DIR"

echo "[$DATE] Starting backup..."

# ── Database dump ─────────────────────────────────────────────────────────────
DUMP_FILE="$BACKUP_DIR/db_$DATE.sql.gz"
PGPASSWORD="$DB_PASSWORD" pg_dump -U "$DB_USER" "$DB_NAME" | gzip > "$DUMP_FILE"
echo "  DB dump: $DUMP_FILE ($(du -sh "$DUMP_FILE" | cut -f1))"

# ── Uploads archive ───────────────────────────────────────────────────────────
if [ -d "$UPLOADS_DIR" ] && [ "$(ls -A "$UPLOADS_DIR" 2>/dev/null)" ]; then
  UPLOADS_FILE="$BACKUP_DIR/uploads_$DATE.tar.gz"
  tar -czf "$UPLOADS_FILE" -C "$UPLOADS_DIR" .
  echo "  Uploads: $UPLOADS_FILE ($(du -sh "$UPLOADS_FILE" | cut -f1))"
else
  echo "  Uploads: empty, skipped"
fi

# ── Clean up old backups ──────────────────────────────────────────────────────
DELETED=$(find "$BACKUP_DIR" -type f -mtime +$KEEP_DAYS -name "*.gz" -print -delete | wc -l)
echo "  Cleaned: $DELETED file(s) older than $KEEP_DAYS days"

echo "  Done."
