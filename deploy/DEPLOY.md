# Deployment Guide

## Go Backend Deployment

### Build binary (on Windows, cross-compile for Ubuntu)

```bash
cd server-go
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o api ./cmd/api
```

Or on Linux/Mac:

```bash
cd server-go
GOOS=linux GOARCH=amd64 go build -o api ./cmd/api
ls -lh api   # expect ~15–25 MB single binary
```

### Transfer binary to VPS

```bash
scp server-go/api user@your-vps:/var/www/inventory-app/server-go/
```

### Apply pending migrations

The `deploy/migrate.sh` shortcut reads DB creds from `server-go/.env` and builds the
connection string for you — no need to paste it by hand:

```bash
bash /var/www/inventory-app/deploy/migrate.sh            # apply all pending (up)
bash /var/www/inventory-app/deploy/migrate.sh version    # current version
bash /var/www/inventory-app/deploy/migrate.sh down 1     # roll back last migration
bash /var/www/inventory-app/deploy/migrate.sh goto 12    # migrate to a specific version
bash /var/www/inventory-app/deploy/migrate.sh force 12   # clear dirty flag after a failed migration
bash /var/www/inventory-app/deploy/migrate.sh create add_foo   # scaffold a new migration pair
```

Or invoke the CLI directly:

```bash
migrate -path server-go/migrations \
  -database "postgres://postgres:password@localhost:5432/inventory_app?sslmode=disable" up
```

### Start / reload with PM2

```bash
# First deploy
pm2 start deploy/ecosystem.config.cjs

# Subsequent deploys
pm2 reload inventory-app
```

---

## Upgrading a server that still has the Node backend

The Express backend (`server/`) has been removed; Go on `:5000` is the only API.
On a server set up before that:

1. **Uploads move automatically.** The next `deploy.sh` moves
   `/var/www/inventory-app/server/uploads` to `/var/www/inventory-app/uploads` and
   leaves a symlink at the old path, so nothing breaks in between. Then point the
   live config at the new folder and reload:
   ```bash
   # PM2 env: UPLOADS_DIR: '/var/www/inventory-app/uploads'
   nano /var/www/inventory-app/deploy/ecosystem.config.cjs
   # Nginx: alias /var/www/inventory-app/uploads/;
   sudo nano /etc/nginx/sites-available/inventory-app
   sudo nginx -t && sudo systemctl reload nginx
   bash /var/www/inventory-app/deploy/deploy.sh --no-pull --no-migrate --no-client
   ```
   Once both are updated, the symlink `server/uploads` can be removed. Update the
   copy of `backup.sh` in cron too if it was copied rather than run in place.
2. **Remove the legacy process** if it still exists:
   ```bash
   sudo pm2 delete inventory-app-legacy; sudo pm2 save
   ```
3. Whatever remains in `server/` (`node_modules`, `config.json`) is unused and
   gitignored; delete it when convenient.

> **PM2 runs as `root` here, not as the login user.** `/root/.pm2` owns the real
> `inventory-app`; always check it with `sudo pm2 list`. A plain `pm2 list` talks
> to the login user's own daemon, which is a different (empty) world.
>
> This matters because a duplicate entry under the login user once crash-looped
> **428,204 times** on `bind: address already in use` — it could never win `:5000`
> from root's healthy instance. Diagnosis was slow because `ss -lptn` run as a
> non-root user shows an *empty* Process column for a socket owned by root, so the
> port holder looks like an untraceable ghost. Re-check with `sudo` before
> concluding a process is orphaned.

After any deploy, check health and run the smoke tests:

```bash
curl http://localhost:5000/api/health
bash /var/www/inventory-app/server-go/scripts/verify.sh
```

---

## 1. Server setup (Ubuntu 22.04)

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install Node.js 20
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs

# Install PostgreSQL
sudo apt install -y postgresql postgresql-contrib

# Install Nginx
sudo apt install -y nginx

# Install PM2 globally
sudo npm install -g pm2

# Create log directory
sudo mkdir -p /var/log/inventory-app
sudo chown $USER:$USER /var/log/inventory-app
```

## 2. Database setup

```bash
sudo -u postgres psql
```
```sql
CREATE DATABASE inventory_app;
CREATE USER postgres WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE inventory_app TO postgres;
\q
```
The schema is built entirely by the migrations, once the application files are in
place (step 3) and `server-go/.env` holds the DB credentials:

```bash
bash /var/www/inventory-app/deploy/migrate.sh        # applies 001_initial onward

# No migration creates a user: create the first admin, then log in and change it
cd /var/www/inventory-app/server-go
go run ./cmd/reset-password -create-admin admin '<a-strong-password>'
```

## 3. Deploy application files

```bash
sudo mkdir -p /var/www/inventory-app
sudo chown $USER:$USER /var/www/inventory-app

# Copy or git clone your project
git clone <your-repo> /var/www/inventory-app

# Build the Go API
cd /var/www/inventory-app/server-go
go build -o api ./cmd/api

# Build the React frontend
cd /var/www/inventory-app/client
npm install
npm run build
# Built files will be in client/dist/
```

## 4. Configure environment

Copy the PM2 template and fill in your values:

```bash
cp /var/www/inventory-app/deploy/ecosystem.config.example.cjs /var/www/inventory-app/deploy/ecosystem.config.cjs
nano /var/www/inventory-app/deploy/ecosystem.config.cjs
```

Set `DB_PASSWORD` to your PostgreSQL password and `JWT_SECRET` to a long random string:

```bash
openssl rand -hex 48
```

`ecosystem.config.cjs` is gitignored and never committed — it stays only on the server.

## 5. Start the app with PM2

```bash
cd /var/www/inventory-app
pm2 start deploy/ecosystem.config.cjs --env production
pm2 save                   # persist across reboots
pm2 startup                # follow the printed command to enable on boot
```

## 6. Configure Nginx

```bash
# Edit the config and replace 'your-domain.com' with your actual domain
sudo cp /var/www/inventory-app/deploy/nginx.conf /etc/nginx/sites-available/inventory-app
sudo ln -s /etc/nginx/sites-available/inventory-app /etc/nginx/sites-enabled/
sudo nginx -t              # test config
sudo systemctl reload nginx
```

## 7. SSL certificate (free via Let's Encrypt)

```bash
sudo apt install -y certbot python3-certbot-nginx
sudo certbot --nginx -d your-domain.com
# Auto-renewal is set up automatically
```

## 8. Set up daily backups

```bash
chmod +x /var/www/inventory-app/deploy/backup.sh

# Edit backup.sh and set DB_PASSWORD

# Add to crontab (runs at 2am daily)
crontab -e
# Add this line:
# 0 2 * * * /var/www/inventory-app/deploy/backup.sh >> /var/log/inventory-app/backup.log 2>&1
```

## Useful PM2 commands

```bash
pm2 status                 # check app status
pm2 logs inventory-app     # view live logs
pm2 restart inventory-app  # restart after code changes
pm2 monit                  # live CPU/memory dashboard
```

## Updating the app

```bash
bash /var/www/inventory-app/deploy/deploy.sh
```

The script pulls, migrates, builds the frontend, builds the Go binary, then
restarts PM2. Skip individual steps with `--no-pull`, `--no-migrate`, or
`--no-client` (backend-only change). Equivalent by hand:

```bash
cd /var/www/inventory-app
git pull
migrate -path server-go/migrations -database "$DATABASE_URL" up
cd client && npm ci && npm run build
cd ../server-go && go build -o api ./cmd/api
pm2 restart inventory-app
```

## First time running migrations on an existing database

If the database already has the tables but golang-migrate has never run against
it, record the version the schema already matches instead of re-running it:

```bash
bash /var/www/inventory-app/deploy/migrate.sh force <version>   # sets the version, runs no SQL
bash /var/www/inventory-app/deploy/migrate.sh                   # applies everything after it
```

`deploy.sh` runs pending migrations on every deploy from then on.
