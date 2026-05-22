# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Start Dev Servers
- **Windows (both at once)**: `start.bat`
- **Frontend only**: `cd client && npm run dev` → http://localhost:5173
- **Backend only**: `cd server && npm run dev` → http://localhost:5000

### Frontend
```bash
cd client
npm run dev        # Vite dev server with HMR
npm run build      # Production build to client/dist/
npm run lint       # ESLint (flat config, ESLint 9)
npm run preview    # Preview production build
```

### Backend
```bash
cd server
npm run dev        # Node with --watch (auto-restart on change)
npm start          # Production (no watch)
```

### Database
```bash
# Create schema
psql -U postgres -d inventory_app -f server/schema.sql

# Load seed data
psql -U postgres -d inventory_app -f server/seed.sql

# Reset admin password (if locked out)
node server/reset-password.js
```

## Architecture Overview

### Monorepo Structure
- `client/` — React 19 SPA (Vite)
- `server/` — Node.js/Express REST API (single file: `server/index.js`)
- `deploy/` — PM2 config, Nginx config, backup script

### Key Architectural Decisions

**Backend is a monolith**: All 40+ routes, middleware, and business logic live in `server/index.js` (~3271 lines). There is no ORM — only raw parameterized SQL via `pg.Pool`. Do not introduce new files for routes; keep them in `index.js`.

**No global state on the frontend**: All state is local component `useState`. There is no Redux, Zustand, or Context API. Data is fetched per-component in `useEffect`. Do not introduce global state.

**Auth flow**: JWT (8h expiry) stored in `localStorage`. `client/src/api.js` has Axios interceptors that auto-refresh on 401 and queue concurrent failing requests. On refresh failure, it clears localStorage and redirects to `/login`. Token revocation uses a `token_blocklist` table with `jti`.

**Financial data**: Currency is IDR, stored as BigInt (integer cents). Always use `Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR' })` for display. Inventory uses FIFO/weighted-average costing tracked in `stock_history.value`. Account balances (`accounts.balance`) are updated in real time on every transaction.

**Multi-step DB operations**: Use `pool.connect()` + `client.query('BEGIN/COMMIT/ROLLBACK')` transactions. Single queries use `pool.query()` directly.

**Activity logging**: Every mutation must call `logActivity()` in the route handler. This is the audit trail.

### Request Flow
```
React page → api.js (Axios + JWT) → Express (auth middleware) → route handler → pool.query() → PostgreSQL
```

### Role-Based Access
- **admin**: Full access to all routes
- **staff**: No access to items CRUD, warehouses, vendors, accounts, users, activity log, or reports
- Route-level enforcement in `server/index.js` via `requireAdmin` middleware

### Frontend Routing
`client/src/App.jsx` defines all routes. The `RequireAuth` HOC wraps protected routes. Pages are one file per route in `client/src/pages/`. Admin-only routes additionally check role in both the UI and the API.

### File Uploads
Invoice photos use Multer with disk storage to `server/uploads/`. Served via Nginx `/uploads/` alias in production.

### POS Import
Sales can be imported from Excel files. The client parses `.xlsx` with the `xlsx` library before sending structured JSON to the server. Server-side also has xlsx for any server-based parsing.

## Environment Variables

The server requires a `.env` file in `server/`:
```
DB_HOST=localhost
DB_PORT=5432
DB_NAME=inventory_app
DB_USER=postgres
DB_PASSWORD=seesaw
JWT_SECRET=inventory_secret_change_in_prod
NODE_ENV=development
```

## Production Deployment

Managed with PM2 + Nginx. See `deploy/DEPLOY.md` for step-by-step instructions.
- PM2 config: `deploy/ecosystem.config.cjs`
- Nginx config: `deploy/nginx.conf` (reverse proxy `/api/` to port 5000, serves `client/dist/` as static)
- Backups: `deploy/backup.sh` (pg_dump + uploads, run via cron)

## Locale & Conventions

- All UI text is in **Indonesian** (`id-ID`)
- UUIDs as primary keys everywhere (`gen_random_uuid()` in SQL)
- ISO 8601 dates in DB, formatted for display in UI
- Timestamps use `TIMESTAMPTZ` (UTC)
- Hard deletes only (no soft delete pattern)
- Default credentials: `admin` / `admin123`
