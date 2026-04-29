# Deployment Guide

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
# Run schema
psql -U postgres -d inventory_app -f /var/www/inventory-app/server/schema.sql
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

Edit `deploy/ecosystem.config.cjs` and set:
- `JWT_SECRET` — a long random string (run: `node -e "console.log(require('crypto').randomBytes(48).toString('hex'))"`)
- `DB_PASSWORD` — your PostgreSQL password

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
cd ../client && npm install && npm run build
pm2 restart inventory-app
```
