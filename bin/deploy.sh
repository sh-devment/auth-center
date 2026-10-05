#!/usr/bin/env bash

set -euo pipefail

# Всё app-специфичное приходит из окружения (GitHub Actions vars/secrets):
#   APP      — имя приложения: бинарь bin/$APP, unit и файлы в /opt/$APP
#   ENV_KEYS — список переменных (через пробел), которые попадут в $APP.env
#   PORT     — порт для health check (обязан быть в ENV_KEYS)

: "${APP:?APP is not set}"
: "${ENV_KEYS:?ENV_KEYS is not set}"

# APP участвует в путях и rm -rf — пускаем только безопасные имена.
[[ "$APP" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || {
  echo "Invalid APP name: '$APP'" >&2
  exit 1
}

APP_DIR="/opt/$APP"

BIN="$APP_DIR/$APP"
SERVICE="$APP_DIR/$APP.service"
ENV_FILE="$APP_DIR/$APP.env"
BACKUP="$APP_DIR/last-deploy-backup"

echo "=== Deploy $APP ==="

# ── Validate ──────────────────────────────────────────────

test -f "bin/$APP"

# Проверяем наличие конфигурации, не выводя значения.
for key in $ENV_KEYS; do
  [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || {
    echo "Invalid env key: '$key'" >&2
    exit 1
  }
  [ -n "${!key:-}" ] || {
    echo "Missing required variable: $key" >&2
    exit 1
  }
done

test -n "${PORT:-}"

# ── Backup current deployment ─────────────────────────────

rm -rf "$BACKUP"
mkdir -p "$BACKUP"

[ ! -f "$BIN" ] ||
  cp -a "$BIN" "$BACKUP/$APP"

[ ! -f "$SERVICE" ] ||
  cp -a "$SERVICE" "$BACKUP/$APP.service"

[ ! -f "$ENV_FILE" ] ||
  cp -a "$ENV_FILE" "$BACKUP/$APP.env"

# ── Environment ───────────────────────────────────────────

umask 077

# APP пишем всегда первым — для сверки при траблшутинге,
# приложению он не нужен.
printf 'APP=%s\n' "$APP" > "$ENV_FILE.new"
for key in $ENV_KEYS; do
  [ "$key" != APP ] || continue
  printf '%s=%s\n' "$key" "${!key}" >> "$ENV_FILE.new"
done

mv "$ENV_FILE.new" "$ENV_FILE"

# ── Binary ────────────────────────────────────────────────

install -m 0755 "bin/$APP" "$BIN.new"
mv "$BIN.new" "$BIN"

# ── Service ───────────────────────────────────────────────

# Unit генерируется из $APP — в репозитории его держать не нужно.
cat > "$SERVICE.new" <<EOF
[Unit]
Description=$APP
After=network.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
EnvironmentFile=$ENV_FILE
ExecStart=$BIN

Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

chmod 0644 "$SERVICE.new"
mv "$SERVICE.new" "$SERVICE"

# ── Restart ───────────────────────────────────────────────

sudo /usr/bin/systemctl daemon-reload
sudo /usr/bin/systemctl enable "$APP.service"
sudo /usr/bin/systemctl restart "$APP.service"

# ── Health check ──────────────────────────────────────────

OK=false

for i in $(seq 1 15); do
  if curl \
    --fail \
    --silent \
    --max-time 2 \
    "http://127.0.0.1:$PORT/" >/dev/null
  then
    OK=true
    break
  fi

  sleep 1
done

# ── Success ───────────────────────────────────────────────

if [ "$OK" = true ]; then
  sudo /usr/bin/systemctl is-active "$APP.service"

  echo
  echo "Version:"
  "$BIN" --version

  echo
  echo "=== Deploy successful ==="

  exit 0
fi

# ── Rollback ──────────────────────────────────────────────

echo
echo "=== Health check failed. Rolling back ==="

if [ -f "$BACKUP/$APP" ]; then
  cp -a "$BACKUP/$APP" "$BIN"
fi

if [ -f "$BACKUP/$APP.service" ]; then
  cp -a "$BACKUP/$APP.service" "$SERVICE"
fi

if [ -f "$BACKUP/$APP.env" ]; then
  cp -a "$BACKUP/$APP.env" "$ENV_FILE"
fi

sudo /usr/bin/systemctl daemon-reload

# На первом deploy предыдущей версии может вообще не быть.
if [ -f "$BACKUP/$APP" ]; then
  sudo /usr/bin/systemctl restart "$APP.service"

  echo "=== Previous application deployment restored ==="
else
  echo "=== First deployment failed; no previous version to restore ==="
fi

exit 1
