# Inventory App

A full-stack inventory and business management system for small-to-medium businesses. Handles stock management, purchasing, sales, production, branch operations, and financial reporting — all in one place.

Built with React, Node.js/Express, and PostgreSQL.

---

## Features

### Inventory & Stock
- Multi-warehouse stock tracking with FIFO costing
- Stock transfers between warehouses
- Stock opname (physical count) with variance reporting
- Full stock movement history per item
- Inventory value reports

### Purchasing & Invoices
- Purchase and expense invoices with line items
- Partial and full payment tracking per invoice
- Overdue invoice alerts on the dashboard
- Invoice photo/PDF attachment support
- Vendor payment history

### Sales & POS
- Manual sales recording to cash/bank accounts
- POS import from Excel (supports commission splits, discount accounts, per-category breakdown)
- Sales filtering by account and date

### Production & Recipes
- Recipe management with ingredients
- Production runs that consume ingredients and add finished goods to stock

### Branch & Division Management
- Multi-branch, multi-division structure
- Each division automatically gets revenue, expense, and discount GL accounts
- Dispatch management (warehouse → branch)

### Financial
- Full chart of accounts (asset, liability, equity, revenue, expense)
- Manual journal adjustments
- Financial report (P&L style)
- Expense summary report by branch/division
- Account balance tracking updated in real time

### Administration
- Role-based access: **admin** (full access) and **staff** (restricted)
- User management with bcrypt password hashing
- Activity log with pagination, date filters, CSV export, and bulk delete
- Rate limiting on all API endpoints

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Frontend | React 19, React Router 7, Vite |
| Backend | Node.js, Express 5 |
| Database | PostgreSQL |
| Auth | JWT (jsonwebtoken), bcrypt |
| File uploads | Multer |
| Excel/POS import | xlsx |
| Process manager | PM2 (production) |
| Reverse proxy | Nginx (production) |

---

## Project Structure

```
inventory-app/
├── client/                  # React frontend
│   ├── src/
│   │   ├── pages/           # One file per page/route
│   │   ├── api.js           # Axios API helpers
│   │   └── App.jsx          # Router and layout
│   └── package.json
├── server/
│   ├── index.js             # Express app and all API routes
│   ├── schema.sql           # Database schema
│   ├── seed.sql             # Seed data
│   ├── reset-password.js    # CLI script for password recovery
│   └── package.json
├── deploy/
│   ├── nginx.conf           # Nginx reverse proxy config
│   ├── ecosystem.config.cjs # PM2 process config
│   ├── backup.sh            # Automated DB + uploads backup
│   └── DEPLOY.md            # Step-by-step deployment guide
└── start.bat                # Windows dev launcher
```

---

## Getting Started (Local Development)

### Prerequisites

- Node.js 20+
- PostgreSQL 14+

### 1. Clone the repository

```bash
git clone <your-repo-url>
cd inventory-app
```

### 2. Set up the database

```bash
psql -U postgres -c "CREATE DATABASE inventory_app;"
psql -U postgres -d inventory_app -f server/schema.sql
psql -U postgres -d inventory_app -f server/seed.sql   # optional seed data
```

### 3. Configure the server

Edit `server/index.js` and update the database connection, or set environment variables:

```bash
DB_HOST=localhost
DB_PORT=5432
DB_NAME=inventory_app
DB_USER=postgres
DB_PASSWORD=your_password
JWT_SECRET=your_long_random_secret
```

### 4. Install dependencies

```bash
cd server && npm install
cd ../client && npm install
```

### 5. Start the development servers

**Windows:**
```bat
start.bat
```

**Manual:**
```bash
# Terminal 1 — backend (port 5000)
cd server && npm run dev

# Terminal 2 — frontend (port 5173)
cd client && npm run dev
```

Open [http://localhost:5173](http://localhost:5173) in your browser.

---

## Default Login

After running the seed, log in with:

| Field | Value |
|-------|-------|
| Username | `admin` |
| Password | `admin123` |

> Change the admin password immediately after first login.

---

## Password Recovery

If an admin forgets their password, run this script directly on the server:

```bash
node server/reset-password.js <username> <new-password>
```

---

## API Overview

All endpoints are prefixed with `/api`. All routes except `/api/auth/login` require a Bearer token.

| Group | Routes |
|-------|--------|
| Auth | `POST /auth/login`, `POST /auth/logout`, `POST /auth/refresh` |
| Users | `GET/POST /users`, `PUT/DELETE /users/:id` |
| Items | `GET/POST /items`, `PUT/DELETE /items/:id`, `GET /items/:id/history` |
| Inventory | `GET/POST /inventory`, `PUT/DELETE /inventory/:id` |
| Warehouses | `GET/POST /warehouses`, `PUT/DELETE /warehouses/:id` |
| Vendors | `GET/POST /vendors`, `PUT/DELETE /vendors/:id`, `GET /vendors/:id/history` |
| Invoices | `GET/POST /invoices`, `PUT/DELETE /invoices/:id`, `POST /invoices/:id/pay` |
| Stock Transfers | `GET/POST /stock-transfers`, `GET /stock-transfers/group/:id` |
| Stock Opname | `GET/POST /stock-opname`, `GET /stock-opname/:id` |
| Dispatches | `GET/POST /dispatches`, `GET /dispatches/:id` |
| Recipes | `GET/POST /recipes`, `PUT/DELETE /recipes/:id` |
| Productions | `GET/POST /productions` |
| Sales | `GET/POST /sales`, `DELETE /sales/:id` |
| POS Import | `POST /pos-import/parse`, `POST /pos-import/confirm`, `GET /pos-import` |
| Branches | `GET/POST /branches`, `PUT/DELETE /branches/:id` |
| Accounts | `GET/POST /accounts`, `PUT/DELETE /accounts/:id` |
| Account Adjustments | `GET/POST /account-adjustments` |
| Activity Log | `GET /activity-log`, `GET /activity-log/export`, `DELETE /activity-log` |
| Reports | `GET /reports/financial`, `GET /reports/inventory-value`, `GET /reports/expense-summary` |
| Stats | `GET /stats` |

---

## Database Schema

The database uses 24 tables organized around these domains:

- **Auth:** `users`, `token_blocklist`
- **Accounts (GL):** `accounts`
- **Organization:** `warehouses`, `branches`, `divisions`, `division_categories`
- **Catalog:** `items`, `vendors`
- **Inventory:** `inventory`, `stock_history`, `stock_transfers`, `stock_opname`, `stock_opname_items`
- **Purchasing:** `invoices`, `invoice_items`
- **Sales:** `sales`, `pos_imports`, `pos_import_lines`
- **Production:** `recipes`, `recipe_ingredients`, `productions`
- **Operations:** `dispatches`, `dispatch_items`
- **Audit:** `activity_log`, `account_adjustments`

---

## Production Deployment

See [`deploy/DEPLOY.md`](deploy/DEPLOY.md) for a full step-by-step guide covering:

- Ubuntu server setup
- PostgreSQL configuration
- Nginx reverse proxy with SSL (Let's Encrypt)
- PM2 process management
- Automated daily backups

**Recommended minimum specs:** 2 vCPU, 2GB RAM, 40GB SSD

---

## User Roles

| Permission | Admin | Staff |
|------------|:-----:|:-----:|
| View all pages | ✓ | ✓ |
| Create/edit records | ✓ | ✓ |
| Edit invoices | ✓ | ✓ |
| Delete records | ✓ | — |
| Manage users | ✓ | — |
| Manage vendors/accounts | ✓ | — |
| Financial reports | ✓ | — |
| Activity log | ✓ | — |
| Clear activity log | ✓ | — |

---

## License

MIT
