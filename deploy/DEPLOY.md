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

## Production Cutover: Express → Go

### Prerequisites

- Go binary deployed at `/var/www/inventory-app/server-go/api`
- Both PM2 entries running: `inventory-app` on `:5000`, `inventory-app-legacy` on `:5001`
- Nginx already proxies `/api/` to `:5000`

### Steps

1. Verify Go server is healthy:
   ```bash
   curl http://localhost:5000/api/health
   ```
2. Run smoke tests:
   ```bash
   bash /var/www/inventory-app/server-go/scripts/verify.sh
   ```
3. Monitor Go logs for at least 24 hours:
   ```bash
   pm2 logs inventory-app
   ```
4. **If issues found** — roll back to Express immediately:
   ```bash
   pm2 stop inventory-app
   # temporarily route legacy to :5000 via Nginx upstream change, or:
   pm2 restart inventory-app-legacy
   ```
5. After 3+ stable days, decommission the legacy process:
   ```bash
   pm2 stop inventory-app-legacy
   pm2 delete inventory-app-legacy
   pm2 save
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
```bash
# Run schema (first-time setup only)
psql -U postgres -d inventory_app -f /var/www/inventory-app/server/schema.sql

# Run migrations (applies any changes on top of the base schema)
cd /var/www/inventory-app/server
npm run migrate
```

## 3. Deploy application files

```bash
sudo mkdir -p /var/www/inventory-app
sudo chown $USER:$USER /var/www/inventory-app

# Copy or git clone your project
git clone <your-repo> /var/www/inventory-app

# Install server dependencies
cd /var/www/inventory-app/server
npm install --omit=dev

# Build the React frontend
cd /var/www/inventory-app/client
npm install
npm run build
# Built files will be in client/dist/
```

## 4. Configure environment

Copy the config template and fill in your values:

```bash
cp /var/www/inventory-app/server/config.example.json /var/www/inventory-app/server/config.json
nano /var/www/inventory-app/server/config.json
```

Set `db.password` to your PostgreSQL password and `jwtSecret` to a long random string:

```bash
node -e "console.log(require('crypto').randomBytes(48).toString('hex'))"
```

`config.json` is gitignored and never committed — it stays only on the server.

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
cd /var/www/inventory-app
git pull
cd server && npm install --omit=dev
npm run migrate          # apply any new migrations before restarting
cd ../client && npm install && npm run build
pm2 restart inventory-app
```

## First time running migrations on an existing database

If your production DB already has the tables but was set up before the migration runner existed, mark all past migrations as already applied so the runner doesn't re-run them:

```bash
cd /var/www/inventory-app/server
node migrate.js   # creates the schema_migrations table

# Then mark the migrations that were already applied manually:
psql -U postgres -d inventory_app -c "
  INSERT INTO schema_migrations (filename) VALUES
    ('add_enumerations.sql'),
    ('add_template_vendor_warehouse.sql')
  ON CONFLICT DO NOTHING;
"

node migrate.js   # should now print: No pending migrations.
```

From this point on, just run `npm run migrate` after every `git pull`.
