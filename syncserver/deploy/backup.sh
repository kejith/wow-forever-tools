#!/usr/bin/env bash
set -euo pipefail
BACKUP_DIR="/opt/wow-ah-sync/backups"
mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
docker exec wow-ah-sync-db pg_dump -U ahsync -d ahsync | gzip -9 > "${BACKUP_DIR}/ahsync-${STAMP}.sql.gz"
find "$BACKUP_DIR" -name 'ahsync-*.sql.gz' -mtime +14 -delete
