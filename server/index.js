import express from 'express';
import cors from 'cors';
import pg from 'pg';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import { randomUUID } from 'crypto';
import { rateLimit } from 'express-rate-limit';
import multer from 'multer';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const XLSX = require('xlsx');

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const uploadsDir = path.join(__dirname, 'uploads');
if (!fs.existsSync(uploadsDir)) fs.mkdirSync(uploadsDir, { recursive: true });

const storage = multer.diskStorage({
  destination: uploadsDir,
  filename: (req, file, cb) => {
    const ext = path.extname(file.originalname).toLowerCase();
    cb(null, `invoice-${req.params.id}-${Date.now()}${ext}`);
  },
});
const upload = multer({ storage, limits: { fileSize: 20 * 1024 * 1024 } });
const memUpload = multer({ storage: multer.memoryStorage(), limits: { fileSize: 20 * 1024 * 1024 } });

const { Pool } = pg;

const app = express();
const PORT = 5000;
const JWT_SECRET = process.env.JWT_SECRET || 'inventory_secret_change_in_prod';

app.use(cors());
app.use(express.json());
app.use('/uploads', express.static(uploadsDir));

// ── RATE LIMITING ─────────────────────────────────────────────────────────────

const loginLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,   // 15 minutes
  max: 10,                      // 10 attempts per IP per window
  standardHeaders: true,
  legacyHeaders: false,
  message: { error: 'Terlalu banyak percobaan login. Coba lagi dalam 15 menit.' },
});

const apiLimiter = rateLimit({
  windowMs: 60 * 1000,         // 1 minute
  max: 300,                     // 300 requests per IP/minute
  standardHeaders: true,
  legacyHeaders: false,
  message: { error: 'Terlalu banyak permintaan. Coba lagi sebentar lagi.' },
  keyGenerator: (req) => req.user?.id || req.socket.remoteAddress,
});

const pool = new Pool({
  host:     process.env.DB_HOST     || 'localhost',
  port:     process.env.DB_PORT     || 5432,
  database: process.env.DB_NAME     || 'inventory_app',
  user:     process.env.DB_USER     || 'postgres',
  password: process.env.DB_PASSWORD || 'seesaw',
});

// ── AUTH ──────────────────────────────────────────────────────────────────────

function issueToken(user) {
  const jti = randomUUID();
  const token = jwt.sign(
    { id: user.id, username: user.username, role: user.role, jti },
    JWT_SECRET,
    { expiresIn: '8h' }
  );
  return { token, jti };
}

app.post('/api/auth/login', loginLimiter, async (req, res) => {
  const { username, password } = req.body;
  if (!username || !password) return res.status(400).json({ error: 'Username and password required' });
  const { rows } = await pool.query('SELECT * FROM users WHERE username=$1', [username]);
  const user = rows[0];
  if (!user) return res.status(401).json({ error: 'Invalid credentials' });
  const valid = await bcrypt.compare(password, user.password_hash);
  if (!valid) return res.status(401).json({ error: 'Invalid credentials' });
  const { token } = issueToken(user);
  res.json({ token, user: { id: user.id, username: user.username, role: user.role } });
});

app.post('/api/auth/logout', async (req, res) => {
  const header = req.headers.authorization;
  if (header?.startsWith('Bearer ')) {
    try {
      const payload = jwt.verify(header.slice(7), JWT_SECRET);
      if (payload.jti) {
        const exp = new Date(payload.exp * 1000);
        await pool.query(
          'INSERT INTO token_blocklist (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING',
          [payload.jti, exp]
        );
      }
    } catch { /* ignore invalid tokens on logout */ }
  }
  res.status(204).end();
});

app.post('/api/auth/refresh', async (req, res) => {
  const header = req.headers.authorization;
  if (!header?.startsWith('Bearer ')) return res.status(401).json({ error: 'Unauthorized' });
  try {
    const payload = jwt.verify(header.slice(7), JWT_SECRET);
    // Blocklist the old token
    if (payload.jti) {
      const exp = new Date(payload.exp * 1000);
      await pool.query(
        'INSERT INTO token_blocklist (jti, expires_at) VALUES ($1, $2) ON CONFLICT DO NOTHING',
        [payload.jti, exp]
      );
    }
    // Check the user still exists and is active
    const { rows } = await pool.query('SELECT id, username, role FROM users WHERE id=$1', [payload.id]);
    if (!rows.length) return res.status(401).json({ error: 'User not found' });
    const { token } = issueToken(rows[0]);
    res.json({ token, user: { id: rows[0].id, username: rows[0].username, role: rows[0].role } });
  } catch {
    res.status(401).json({ error: 'Invalid or expired token' });
  }
});

async function requireAuth(req, res, next) {
  const header = req.headers.authorization;
  if (!header?.startsWith('Bearer ')) return res.status(401).json({ error: 'Unauthorized' });
  try {
    const payload = jwt.verify(header.slice(7), JWT_SECRET);
    // Check token hasn't been revoked
    if (payload.jti) {
      const { rowCount } = await pool.query('SELECT 1 FROM token_blocklist WHERE jti=$1', [payload.jti]);
      if (rowCount > 0) return res.status(401).json({ error: 'Token has been revoked' });
    }
    req.user = payload;
    next();
  } catch {
    res.status(401).json({ error: 'Invalid or expired token' });
  }
}

function requireAdmin(req, res, next) {
  if (req.user?.role !== 'admin') return res.status(403).json({ error: 'Admin access required' });
  next();
}

app.use('/api', (req, res, next) => {
  if (req.path === '/auth/login' || req.path === '/auth/logout' || req.path === '/auth/refresh') return next();
  requireAuth(req, res, next);
});

// Apply general rate limit to all authenticated API routes
app.use('/api', (req, res, next) => {
  if (req.path === '/auth/login' || req.path === '/auth/logout' || req.path === '/auth/refresh') return next();
  apiLimiter(req, res, next);
});

// ── USERS ─────────────────────────────────────────────────────────────────────

app.get('/api/users', async (req, res) => {
  const { rows } = await pool.query('SELECT id, username, role, created_at FROM users ORDER BY created_at');
  res.json(rows);
});

app.post('/api/users', requireAdmin, async (req, res) => {
  const { username, password, role = 'admin' } = req.body;
  if (!username || !password) return res.status(400).json({ error: 'Username and password required' });
  const hash = await bcrypt.hash(password, 10);
  try {
    const { rows } = await pool.query(
      'INSERT INTO users (username, password_hash, role) VALUES ($1,$2,$3) RETURNING id, username, role, created_at',
      [username, hash, role]
    );
    res.status(201).json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Username already taken' });
    throw e;
  }
});

app.put('/api/users/:id', requireAdmin, async (req, res) => {
  const { username, password, old_password, role } = req.body;
  const fields = [];
  const vals = [];
  if (username) { fields.push(`username=$${fields.length+1}`); vals.push(username); }
  if (role)     { fields.push(`role=$${fields.length+1}`);     vals.push(role); }
  if (password) {
    if (!old_password) return res.status(400).json({ error: 'Password lama wajib diisi untuk mengubah password' });
    const { rows: [u] } = await pool.query('SELECT password_hash FROM users WHERE id=$1', [req.params.id]);
    if (!u) return res.status(404).json({ error: 'Not found' });
    const match = await bcrypt.compare(old_password, u.password_hash);
    if (!match) return res.status(400).json({ error: 'Password lama tidak sesuai' });
    fields.push(`password_hash=$${fields.length+1}`);
    vals.push(await bcrypt.hash(password, 10));
  }
  if (!fields.length) return res.status(400).json({ error: 'Nothing to update' });
  vals.push(req.params.id);
  try {
    const { rows } = await pool.query(
      `UPDATE users SET ${fields.join(',')} WHERE id=$${vals.length} RETURNING id, username, role, created_at`,
      vals
    );
    if (!rows.length) return res.status(404).json({ error: 'Not found' });
    res.json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Username already taken' });
    throw e;
  }
});

app.delete('/api/users/:id', requireAdmin, async (req, res) => {
  if (req.user.id === req.params.id) return res.status(400).json({ error: 'Cannot delete your own account' });
  await pool.query('DELETE FROM users WHERE id=$1', [req.params.id]);
  res.status(204).end();
});

// ── WAREHOUSES ────────────────────────────────────────────────────────────────

app.get('/api/warehouses', async (req, res) => {
  const { rows } = await pool.query(`
    SELECT w.*, a.account_number AS inventory_account_number, a.name AS inventory_account_name
    FROM warehouses w
    LEFT JOIN accounts a ON a.id = w.inventory_account_id
    ORDER BY w.name
  `);
  res.json(rows);
});

app.post('/api/warehouses', requireAdmin, async (req, res) => {
  const { name, account_number } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  if (!account_number) return res.status(400).json({ error: 'account_number is required (must be in 10000–19999 range for inventory assets)' });

  const num = Number(account_number);
  if (num < 10000 || num > 19999) {
    return res.status(400).json({ error: 'Inventory account number must be in the 10000–19999 range (Assets)' });
  }

  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [assetRoot] } = await client.query(
      `SELECT id FROM accounts WHERE account_number = 10000 AND is_system = true LIMIT 1`
    );
    let invAccount;
    try {
      const { rows: [ia] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'asset', 0, $3) RETURNING *`,
        [num, `Persediaan - ${name}`, assetRoot?.id ?? null]
      );
      invAccount = ia;
    } catch (e) {
      if (e.code === '23505') {
        await client.query('ROLLBACK');
        return res.status(409).json({ error: `Account number ${num} already exists` });
      }
      throw e;
    }
    const { rows } = await client.query(
      'INSERT INTO warehouses (name, inventory_account_id) VALUES ($1, $2) RETURNING *',
      [name, invAccount.id]
    );
    await client.query('COMMIT');
    res.status(201).json({ ...rows[0], inventory_account: invAccount });
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/warehouses/:id', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('UPDATE warehouses SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Warehouse not found' });
  res.json(rows[0]);
});

app.delete('/api/warehouses/:id', requireAdmin, async (req, res) => {
  const [inv, invCheck] = await Promise.all([
    pool.query('SELECT 1 FROM inventory WHERE warehouse_id=$1 LIMIT 1', [req.params.id]),
    pool.query('SELECT 1 FROM invoices WHERE warehouse_id=$1 LIMIT 1', [req.params.id]),
  ]);
  if (inv.rows.length || invCheck.rows.length) {
    return res.status(409).json({ error: 'Warehouse is in use by inventory or invoice records' });
  }
  const { rowCount } = await pool.query('DELETE FROM warehouses WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Warehouse not found' });
  res.status(204).send();
});

// ── VENDORS ───────────────────────────────────────────────────────────────────

app.get('/api/vendors', async (req, res) => {
  const { rows } = await pool.query('SELECT * FROM vendors ORDER BY name');
  res.json(rows);
});

app.post('/api/vendors', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('INSERT INTO vendors (name) VALUES ($1) RETURNING *', [name]);
  res.status(201).json(rows[0]);
});

app.put('/api/vendors/:id', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('UPDATE vendors SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Vendor not found' });
  res.json(rows[0]);
});

app.delete('/api/vendors/:id', requireAdmin, async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM invoice_items WHERE vendor_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Vendor is referenced by existing records' });
  const { rowCount } = await pool.query('DELETE FROM vendors WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Vendor not found' });
  res.status(204).send();
});

app.get('/api/vendors/:id/history', async (req, res) => {
  const { id } = req.params;

  const vendorRes = await pool.query('SELECT * FROM vendors WHERE id=$1', [id]);
  if (!vendorRes.rows.length) return res.status(404).json({ error: 'Vendor not found' });

  const { rows: invoices } = await pool.query(
    `SELECT inv.id, inv.invoice_number, inv.date, inv.due_date, inv.payment_status,
            inv.amount_paid, inv.account_id, a.name AS account_name,
            inv.reference_number,
            COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
     FROM invoices inv
     LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
     LEFT JOIN accounts a ON a.id = inv.account_id
     WHERE inv.vendor_id = $1 AND inv.invoice_type = 'purchase'
     GROUP BY inv.id, a.name
     ORDER BY inv.date DESC, inv.created_at DESC`,
    [id]
  );

  const totalInvoiced  = invoices.reduce((s, r) => s + Number(r.total), 0);
  const totalPaid      = invoices.reduce((s, r) => s + Number(r.amount_paid), 0);
  const totalOutstanding = totalInvoiced - totalPaid;

  res.json({
    vendor: vendorRes.rows[0],
    invoices,
    summary: { totalInvoiced, totalPaid, totalOutstanding },
  });
});

// ── ACCOUNTS ─────────────────────────────────────────────────────────────────

app.get('/api/accounts', async (req, res) => {
  const { rows } = await pool.query(`
    SELECT a.*, p.account_number AS parent_number, p.name AS parent_name
    FROM accounts a
    LEFT JOIN accounts p ON p.id = a.parent_id
    ORDER BY COALESCE(a.account_number, 99999), a.name
  `);
  res.json(rows);
});

app.post('/api/accounts', requireAdmin, async (req, res) => {
  const { name, balance = 0, account_number, parent_id } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });

  let acctType = 'asset';
  let num = account_number != null ? Number(account_number) : null;
  if (num != null) {
    if (num < 10000 || num > 59999) return res.status(400).json({ error: 'Account number must be between 10000 and 59999' });
    acctType = typeFromNumber(num);
  }

  try {
    const { rows } = await pool.query(
      `INSERT INTO accounts (name, balance, account_number, account_type, parent_id)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [name, Number(balance), num, acctType, parent_id ?? null]
    );
    res.status(201).json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Account number already exists' });
    throw e;
  }
});

app.put('/api/accounts/:id', requireAdmin, async (req, res) => {
  const { name, balance, account_number, parent_id } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });

  const { rows: [existing] } = await pool.query('SELECT * FROM accounts WHERE id=$1', [req.params.id]);
  if (!existing) return res.status(404).json({ error: 'Account not found' });

  let num = account_number != null ? Number(account_number) : existing.account_number;
  let acctType = num != null ? typeFromNumber(num) : existing.account_type;

  // System accounts: only allow renaming
  if (existing.is_system && num !== existing.account_number) {
    return res.status(403).json({ error: 'Cannot change the account number of a system account' });
  }

  try {
    const { rows } = await pool.query(
      `UPDATE accounts SET name=$1, balance=$2, account_number=$3, account_type=$4, parent_id=$5 WHERE id=$6 RETURNING *`,
      [name, Number(balance ?? existing.balance), num, acctType, parent_id ?? existing.parent_id, req.params.id]
    );
    res.json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Account number already exists' });
    throw e;
  }
});

app.delete('/api/accounts/:id', requireAdmin, async (req, res) => {
  const { rows: [existing] } = await pool.query('SELECT * FROM accounts WHERE id=$1', [req.params.id]);
  if (!existing) return res.status(404).json({ error: 'Account not found' });
  if (existing.is_system) return res.status(403).json({ error: 'Cannot delete system accounts' });

  const [inUse, hasChildren] = await Promise.all([
    pool.query('SELECT 1 FROM invoices WHERE account_id=$1 LIMIT 1', [req.params.id]),
    pool.query('SELECT 1 FROM accounts WHERE parent_id=$1 LIMIT 1', [req.params.id]),
  ]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Account is referenced by existing invoices' });
  if (hasChildren.rows.length) return res.status(409).json({ error: 'Account has sub-accounts and cannot be deleted' });

  await pool.query('DELETE FROM accounts WHERE id=$1', [req.params.id]);
  res.status(204).send();
});

// ── ITEMS ─────────────────────────────────────────────────────────────────────

app.get('/api/items', async (req, res) => {
  const { search, is_stock } = req.query;
  const params = [];
  const conds = [];
  if (search)              { params.push(`%${search}%`); conds.push(`(name ILIKE $${params.length} OR code ILIKE $${params.length})`); }
  if (is_stock !== undefined) { params.push(is_stock === 'true'); conds.push(`is_stock=$${params.length}`); }
  let query = 'SELECT * FROM items';
  if (conds.length) query += ' WHERE ' + conds.join(' AND ');
  const { rows } = await pool.query(query + ' ORDER BY name', params);
  res.json(rows);
});

app.get('/api/items/:id', async (req, res) => {
  const { rows } = await pool.query('SELECT * FROM items WHERE id=$1', [req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Item not found' });
  res.json(rows[0]);
});

app.post('/api/items', requireAdmin, async (req, res) => {
  const { name, code, units, is_stock = true } = req.body;
  if (!name || !code || !Array.isArray(units) || units.length < 1 || units.length > 3) {
    return res.status(400).json({ error: 'name, code, and 1–3 units are required' });
  }
  const { rows } = await pool.query(
    'INSERT INTO items (name, code, units, is_stock) VALUES ($1, $2, $3, $4) RETURNING *',
    [name, code, JSON.stringify(units), is_stock]
  );
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'item', entity_id: rows[0].id, description: `Created item "${name}" (${code})` });
  res.status(201).json(rows[0]);
});

app.put('/api/items/:id', requireAdmin, async (req, res) => {
  const { name, code, units, is_stock } = req.body;
  const { rows } = await pool.query(
    'UPDATE items SET name=$1, code=$2, units=$3, is_stock=COALESCE($4, is_stock) WHERE id=$5 RETURNING *',
    [name, code, JSON.stringify(units), is_stock ?? null, req.params.id]
  );
  if (!rows.length) return res.status(404).json({ error: 'Item not found' });
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'update', entity_type: 'item', entity_id: rows[0].id, description: `Updated item "${name}" (${code})` });
  res.json(rows[0]);
});

app.delete('/api/items/:id', requireAdmin, async (req, res) => {
  const { rows: [item] } = await pool.query('SELECT name, code FROM items WHERE id=$1', [req.params.id]);
  if (!item) return res.status(404).json({ error: 'Item not found' });
  try {
    await pool.query('DELETE FROM items WHERE id=$1', [req.params.id]);
  } catch (err) {
    if (err.code === '23503') {
      return res.status(409).json({ error: 'Barang tidak dapat dihapus karena masih digunakan di invoice, resep, atau pengiriman.' });
    }
    throw err;
  }
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'delete', entity_type: 'item', entity_id: req.params.id, description: `Deleted item "${item.name}" (${item.code})` });
  res.status(204).send();
});

app.get('/api/items/:id/last-price', async (req, res) => {
  const { unit_index } = req.query;
  let q = `SELECT ii.price, ii.unit_index, inv.date
            FROM invoice_items ii
            JOIN invoices inv ON inv.id = ii.invoice_id
            WHERE ii.item_id = $1`;
  const params = [req.params.id];
  if (unit_index !== undefined) { params.push(Number(unit_index)); q += ` AND ii.unit_index = $2`; }
  q += ` ORDER BY inv.date DESC, inv.created_at DESC LIMIT 1`;
  const { rows } = await pool.query(q, params);
  res.json(rows[0] ?? null);
});

app.get('/api/items/:id/history', async (req, res) => {
  const { rows } = await pool.query(
    `SELECT ii.id, ii.quantity, ii.unit_index, ii.price,
            inv.id AS invoice_id, inv.invoice_number, inv.date,
            inv.invoice_type, inv.payment_status,
            inv.branch_id, inv.division_id, inv.dispatch_id,
            b.name AS branch_name, dv.name AS division_name,
            it.units->>(ii.unit_index::TEXT) AS unit_name,
            (ii.quantity * ii.price) AS line_total
     FROM invoice_items ii
     JOIN invoices inv ON inv.id = ii.invoice_id
     JOIN items it ON it.id = ii.item_id
     LEFT JOIN branches b  ON b.id  = inv.branch_id
     LEFT JOIN divisions dv ON dv.id = inv.division_id
     WHERE ii.item_id = $1
     ORDER BY inv.date DESC, inv.created_at DESC`,
    [req.params.id]
  );
  res.json(rows);
});

// ── INVENTORY ─────────────────────────────────────────────────────────────────

const inventorySelect = `
  SELECT inv.*, i.name AS item_name, i.code AS item_code,
         i.units->inv.unit_index->>'name' AS unit_name,
         w.name AS warehouse_name
  FROM inventory inv
  JOIN items i ON i.id = inv.item_id
  JOIN warehouses w ON w.id = inv.warehouse_id
`;

// Run once at startup to add columns if they don't exist yet
pool.query(`
  ALTER TABLE stock_history   ADD COLUMN IF NOT EXISTS source_id    UUID;
  ALTER TABLE stock_history   ADD COLUMN IF NOT EXISTS source_type  TEXT;
  ALTER TABLE stock_transfers ADD COLUMN IF NOT EXISTS group_id     UUID;
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS invoice_type  TEXT NOT NULL DEFAULT 'purchase';
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS photo_path    TEXT;
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS branch_id     UUID REFERENCES branches(id);
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS division_id   UUID REFERENCES divisions(id);
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS dispatch_id   UUID REFERENCES dispatches(id);
  ALTER TABLE invoice_items   ADD COLUMN IF NOT EXISTS description   TEXT;
  ALTER TABLE stock_opname    ADD COLUMN IF NOT EXISTS operator_name TEXT;
  ALTER TABLE items           ADD COLUMN IF NOT EXISTS is_stock         BOOLEAN NOT NULL DEFAULT true;
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS vendor_id         UUID REFERENCES vendors(id);
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS reference_number  TEXT;
  ALTER TABLE stock_opname    ADD COLUMN IF NOT EXISTS pic_name          TEXT;
  ALTER TABLE stock_history   ADD COLUMN IF NOT EXISTS value             BIGINT;
  ALTER TABLE invoices        ADD COLUMN IF NOT EXISTS due_date          DATE;
  ALTER TABLE accounts        ADD COLUMN IF NOT EXISTS account_number INT;
  ALTER TABLE accounts        ADD COLUMN IF NOT EXISTS account_type   TEXT NOT NULL DEFAULT 'asset';
  ALTER TABLE accounts        ADD COLUMN IF NOT EXISTS parent_id      UUID REFERENCES accounts(id);
  ALTER TABLE accounts        ADD COLUMN IF NOT EXISTS is_system      BOOLEAN NOT NULL DEFAULT false;
  ALTER TABLE warehouses      ADD COLUMN IF NOT EXISTS inventory_account_id UUID REFERENCES accounts(id);
  ALTER TABLE branches        ADD COLUMN IF NOT EXISTS revenue_account_id   UUID REFERENCES accounts(id);
  ALTER TABLE branches        ADD COLUMN IF NOT EXISTS expense_account_id   UUID REFERENCES accounts(id);
  ALTER TABLE divisions       ADD COLUMN IF NOT EXISTS revenue_account_id   UUID REFERENCES accounts(id);
  ALTER TABLE divisions       ADD COLUMN IF NOT EXISTS expense_account_id   UUID REFERENCES accounts(id);
  ALTER TABLE sales           ADD COLUMN IF NOT EXISTS branch_id             UUID REFERENCES branches(id);
  ALTER TABLE sales           ADD COLUMN IF NOT EXISTS division_id           UUID REFERENCES divisions(id);
`).then(() => pool.query(`
  CREATE TABLE IF NOT EXISTS pos_imports (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    description TEXT,
    date        DATE NOT NULL,
    source_file TEXT,
    total_amount BIGINT NOT NULL,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ DEFAULT NOW()
  );
  CREATE TABLE IF NOT EXISTS pos_import_lines (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_id UUID REFERENCES pos_imports(id) ON DELETE CASCADE,
    account_id UUID REFERENCES accounts(id),
    label     TEXT NOT NULL,
    amount    BIGINT NOT NULL,
    line_type TEXT NOT NULL
  );
`)).then(() => pool.query(`
  CREATE TABLE IF NOT EXISTS recipes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    output_item_id   UUID REFERENCES items(id),
    batch_size       NUMERIC NOT NULL,
    batch_unit_index INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ DEFAULT NOW()
  );
  CREATE TABLE IF NOT EXISTS recipe_ingredients (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id  UUID REFERENCES recipes(id) ON DELETE CASCADE,
    item_id    UUID REFERENCES items(id),
    quantity   NUMERIC NOT NULL,
    unit_index INT NOT NULL DEFAULT 0
  );
  CREATE TABLE IF NOT EXISTS productions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id       UUID REFERENCES recipes(id),
    warehouse_id    UUID REFERENCES warehouses(id),
    batches         NUMERIC NOT NULL,
    output_quantity NUMERIC NOT NULL,
    date            DATE NOT NULL DEFAULT CURRENT_DATE,
    notes           TEXT,
    created_by      UUID REFERENCES users(id),
    created_at      TIMESTAMPTZ DEFAULT NOW()
  );
`)).then(() => pool.query(`
  ALTER TABLE invoice_items ALTER COLUMN item_id       DROP NOT NULL;
  ALTER TABLE invoice_items ALTER COLUMN vendor_id     DROP NOT NULL;
  ALTER TABLE invoice_items ALTER COLUMN unit_index    DROP NOT NULL;
  ALTER TABLE invoices      ALTER COLUMN payment_method DROP NOT NULL;
  ALTER TABLE invoices      ALTER COLUMN warehouse_id   DROP NOT NULL;
  CREATE UNIQUE INDEX IF NOT EXISTS accounts_number_unique ON accounts (account_number) WHERE account_number IS NOT NULL;
  ALTER TABLE invoices  ADD COLUMN IF NOT EXISTS amount_paid        BIGINT NOT NULL DEFAULT 0;
  ALTER TABLE divisions ADD COLUMN IF NOT EXISTS discount_account_id UUID REFERENCES accounts(id);
  CREATE TABLE IF NOT EXISTS division_categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    division_id UUID REFERENCES divisions(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    UNIQUE(division_id, name)
  );
  CREATE TABLE IF NOT EXISTS token_blocklist (
    jti        TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
  );
  CREATE INDEX IF NOT EXISTS token_blocklist_expires_idx ON token_blocklist (expires_at);
  CREATE TABLE IF NOT EXISTS account_adjustments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     UUID NOT NULL REFERENCES accounts(id),
    amount         BIGINT NOT NULL,
    description    TEXT NOT NULL,
    created_by     UUID REFERENCES users(id),
    created_by_name TEXT,
    created_at     TIMESTAMPTZ DEFAULT NOW()
  );
  CREATE INDEX IF NOT EXISTS account_adjustments_account_idx ON account_adjustments (account_id);
  CREATE INDEX IF NOT EXISTS account_adjustments_created_idx ON account_adjustments (created_at DESC);
`)).then(() => seedCoa())
.catch(e => console.error('Migration error:', e.message));

// Prune expired tokens from blocklist every hour
setInterval(async () => {
  try {
    await pool.query('DELETE FROM token_blocklist WHERE expires_at < NOW()');
  } catch (e) {
    console.error('Blocklist prune error:', e.message);
  }
}, 60 * 60 * 1000);

async function writeHistory(client, { item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_id, source_type, value }) {
  await client.query(
    `INSERT INTO stock_history (item_id, warehouse_id, quantity_change, unit_name, vendor, type, reference, date, source_id, source_type, value)
     VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
    [item_id, warehouse_id, quantity_change, unit_name, vendor || null, type, reference || null, date || 'today', source_id || null, source_type || null, value ?? null]
  );
}

async function logActivity(client, { user_id, username, action, entity_type, entity_id, description }) {
  await client.query(
    `INSERT INTO activity_log (user_id, username, action, entity_type, entity_id, description)
     VALUES ($1, $2, $3, $4, $5, $6)`,
    [user_id || null, username, action, entity_type, entity_id || null, description]
  );
}

// Idempotent COA seed — safe to run on every startup; creates missing accounts only
async function seedCoa() {
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Insert account only if the account_number doesn't already exist
    async function ensure(num, name, type, parentId = null, system = true) {
      const { rows: [ex] } = await client.query(
        `SELECT id FROM accounts WHERE account_number = $1 LIMIT 1`, [num]
      );
      if (ex) return ex.id;
      const { rows: [cr] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id, is_system)
         VALUES ($1, $2, $3, 0, $4, $5) RETURNING id`,
        [num, name, type, parentId, system]
      );
      return cr.id;
    }

    // Root categories
    const assetId = await ensure(10000, 'Aset',       'asset');
    const liabId  = await ensure(20000, 'Kewajiban',  'liability');
                    await ensure(30000, 'Ekuitas',    'equity');
                    await ensure(40000, 'Pendapatan', 'revenue');
                    await ensure(50000, 'Beban',      'expense');

    // Liability sub-accounts
    await ensure(20100, 'Utang Usaha', 'liability', liabId);

    // Cash & Cash Equivalents group under Assets
    const cashGroupId = await ensure(12000, 'Kas dan Setara Kas', 'asset', assetId);

    // Re-parent any pre-COA orphan asset accounts (no number, not system, no parent) into 12000
    await client.query(`
      UPDATE accounts
      SET parent_id = $1
      WHERE account_type = 'asset'
        AND is_system     = false
        AND account_number IS NULL
        AND parent_id     IS NULL
    `, [cashGroupId]);

    await client.query('COMMIT');
  } catch (e) {
    await client.query('ROLLBACK');
    console.error('COA seed error:', e.message);
  } finally {
    client.release();
  }
}

// Helper: derive account_type from a 5-digit account_number
function typeFromNumber(n) {
  if (n < 20000) return 'asset';
  if (n < 30000) return 'liability';
  if (n < 40000) return 'equity';
  if (n < 50000) return 'revenue';
  return 'expense';
}

// Helper: get the default Accounts Payable account (20100)
async function getApAccount(client) {
  const { rows: [ap] } = await client.query(
    `SELECT id FROM accounts WHERE account_number = 20100 AND is_system = true LIMIT 1`
  );
  return ap ?? null;
}

async function getInventoryAccount(client, warehouseId) {
  if (!warehouseId) return null;
  const { rows: [wh] } = await client.query(
    'SELECT inventory_account_id FROM warehouses WHERE id=$1', [warehouseId]
  );
  return wh?.inventory_account_id ? { id: wh.inventory_account_id } : null;
}

async function getExpenseAccount(client, divisionId, branchId) {
  if (divisionId) {
    const { rows: [d] } = await client.query('SELECT expense_account_id FROM divisions WHERE id=$1', [divisionId]);
    if (d?.expense_account_id) return { id: d.expense_account_id };
  }
  if (branchId) {
    const { rows: [b] } = await client.query('SELECT expense_account_id FROM branches WHERE id=$1', [branchId]);
    if (b?.expense_account_id) return { id: b.expense_account_id };
  }
  return null;
}

async function getRevenueAccount(client, divisionId, branchId) {
  if (divisionId) {
    const { rows: [d] } = await client.query('SELECT revenue_account_id FROM divisions WHERE id=$1', [divisionId]);
    if (d?.revenue_account_id) return { id: d.revenue_account_id };
  }
  if (branchId) {
    const { rows: [b] } = await client.query('SELECT revenue_account_id FROM branches WHERE id=$1', [branchId]);
    if (b?.revenue_account_id) return { id: b.revenue_account_id };
  }
  return null;
}

app.get('/api/inventory', async (req, res) => {
  const { search, warehouse_id, date_from, date_to } = req.query;
  const params = [];
  const conditions = [];
  if (search) {
    params.push(`%${search}%`);
    conditions.push(`(i.name ILIKE $${params.length} OR i.code ILIKE $${params.length})`);
  }
  if (warehouse_id && warehouse_id !== 'all') {
    params.push(warehouse_id);
    conditions.push(`inv.warehouse_id = $${params.length}`);
  }
  if (date_from) { params.push(date_from); conditions.push(`inv.date >= $${params.length}`); }
  if (date_to)   { params.push(date_to);   conditions.push(`inv.date <= $${params.length}`); }
  const where = conditions.length ? ' WHERE ' + conditions.join(' AND ') : '';
  const { rows } = await pool.query(inventorySelect + where + ' ORDER BY inv.date DESC, i.name', params);
  res.json(rows);
});

app.get('/api/inventory/:id', async (req, res) => {
  const { rows } = await pool.query(inventorySelect + ' WHERE inv.id=$1', [req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Inventory record not found' });
  res.json(rows[0]);
});

app.post('/api/inventory', async (req, res) => {
  const { item_id, warehouse_id, quantity, unit_index, value, date } = req.body;
  if (!item_id || !warehouse_id || quantity == null || unit_index == null || value == null) {
    return res.status(400).json({ error: 'All fields are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows } = await client.query(
      `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
       VALUES ($1, $2, $3, $4, $5, $6) RETURNING *`,
      [item_id, warehouse_id, quantity, unit_index, value, date || 'today']
    );
    const { rows: [item] } = await client.query('SELECT name, units FROM items WHERE id=$1', [item_id]);
    const unit_name = item.units[Number(unit_index)]?.name ?? '';
    await writeHistory(client, {
      item_id, warehouse_id, quantity_change: Number(quantity),
      unit_name, type: 'manual_in', date: date || null,
    });
    const { rows: [wh] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [warehouse_id]);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'inventory', entity_id: rows[0].id, description: `Added ${quantity} ${unit_name} of "${item.name}" to ${wh.name}` });
    await client.query('COMMIT');
    res.status(201).json(rows[0]);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/inventory/:id', async (req, res) => {
  const { item_id, warehouse_id, quantity, unit_index, value, date } = req.body;
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [old] } = await client.query('SELECT * FROM inventory WHERE id=$1', [req.params.id]);
    if (!old) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Inventory record not found' }); }
    const { rows } = await client.query(
      `UPDATE inventory SET item_id=$1, warehouse_id=$2, quantity=$3, unit_index=$4, value=$5, date=$6
       WHERE id=$7 RETURNING *`,
      [item_id, warehouse_id, quantity, unit_index, value, date, req.params.id]
    );
    const delta = Number(quantity) - Number(old.quantity);
    const { rows: [item] } = await client.query('SELECT name, units FROM items WHERE id=$1', [item_id]);
    const unit_name = item.units[Number(unit_index)]?.name ?? '';
    if (delta !== 0) {
      await writeHistory(client, {
        item_id, warehouse_id, quantity_change: delta,
        unit_name, type: 'manual_adjustment', date: date || null,
      });
    }
    const { rows: [wh] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [warehouse_id]);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'update', entity_type: 'inventory', entity_id: req.params.id, description: `Updated inventory: "${item.name}" at ${wh.name} → ${quantity} ${unit_name}` });
    await client.query('COMMIT');
    res.json(rows[0]);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.delete('/api/inventory/:id', async (req, res) => {
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [old] } = await client.query(
      inventorySelect + ' WHERE inv.id=$1', [req.params.id]
    );
    if (!old) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Inventory record not found' }); }
    await client.query('DELETE FROM inventory WHERE id=$1', [req.params.id]);
    await writeHistory(client, {
      item_id: old.item_id, warehouse_id: old.warehouse_id,
      quantity_change: -Number(old.quantity), unit_name: old.unit_name,
      type: 'manual_out', date: old.date,
    });
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'delete', entity_type: 'inventory', entity_id: req.params.id, description: `Deleted inventory record: ${old.quantity} ${old.unit_name} of "${old.item_name}" from ${old.warehouse_name}` });
    await client.query('COMMIT');
    res.status(204).send();
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── STOCK HISTORY ─────────────────────────────────────────────────────────────

app.get('/api/stock-history/:itemId', async (req, res) => {
  const { warehouse_id, type, date_from, date_to, all } = req.query;
  const params = [req.params.itemId];
  const conditions = [];
  if (warehouse_id) { params.push(warehouse_id); conditions.push(`sh.warehouse_id=$${params.length}`); }
  if (type)         { params.push(type);         conditions.push(`sh.type=$${params.length}`); }
  if (date_from)    { params.push(date_from);    conditions.push(`sh.date>=$${params.length}`); }
  if (date_to)      { params.push(date_to);      conditions.push(`sh.date<=$${params.length}`); }
  const where = conditions.length ? 'AND ' + conditions.join(' AND ') : '';
  const limitClause = all === 'true' ? '' : 'LIMIT 10';
  const { rows } = await pool.query(
    `SELECT sh.*, w.name AS warehouse_name
     FROM stock_history sh
     JOIN warehouses w ON w.id = sh.warehouse_id
     WHERE sh.item_id=$1 ${where}
     ORDER BY sh.created_at DESC ${limitClause}`,
    params
  );
  res.json(rows);
});

// ── STOCK OPNAME ──────────────────────────────────────────────────────────────

app.get('/api/stock-opname', async (req, res) => {
  const { rows: opnames } = await pool.query(
    `SELECT so.*, w.name AS warehouse_name, u.username AS performed_by_name
     FROM stock_opname so
     JOIN warehouses w  ON w.id = so.warehouse_id
     LEFT JOIN users u  ON u.id = so.performed_by
     ORDER BY so.performed_at DESC`
  );
  const { rows: items } = await pool.query(
    `SELECT soi.*, i.name AS item_name, i.code AS item_code
     FROM stock_opname_items soi JOIN items i ON i.id = soi.item_id`
  );
  const byOpname = {};
  for (const it of items) {
    if (!byOpname[it.opname_id]) byOpname[it.opname_id] = [];
    byOpname[it.opname_id].push(it);
  }
  res.json(opnames.map(o => ({ ...o, items: byOpname[o.id] || [] })));
});

app.post('/api/stock-opname', async (req, res) => {
  const { warehouse_id, notes, operator_name, pic_name, items } = req.body;
  if (!warehouse_id || !Array.isArray(items) || !items.length) {
    return res.status(400).json({ error: 'Warehouse and items are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Ensure "Stock Waste" account exists
    let { rows: [wasteAcct] } = await client.query(`SELECT id FROM accounts WHERE name='Stock Waste'`);
    if (!wasteAcct) {
      const ins = await client.query(`INSERT INTO accounts (name, balance) VALUES ('Stock Waste', 0) RETURNING id`);
      wasteAcct = ins.rows[0];
    }

    const { rows: [opname] } = await client.query(
      `INSERT INTO stock_opname (warehouse_id, notes, operator_name, pic_name, performed_by) VALUES ($1,$2,$3,$4,$5) RETURNING *`,
      [warehouse_id, notes || null, operator_name || null, pic_name || null, req.user.id]
    );

    let totalWaste = 0;
    for (const it of items) {
      const { inventory_id, item_id, unit_index, actual_quantity } = it;
      const { rows: [inv] } = await client.query(
        `SELECT inv.*, i.name AS item_name, i.units->inv.unit_index->>'name' AS unit_name
         FROM inventory inv JOIN items i ON i.id=inv.item_id WHERE inv.id=$1`,
        [inventory_id]
      );
      if (!inv) continue;

      const recorded = Number(inv.quantity);
      const actual   = Number(actual_quantity);
      const diff     = actual - recorded;
      if (diff === 0) continue;

      const absValueChange = Math.round(Math.abs(Number(inv.value)) * Math.abs(diff) / recorded);
      const wasteValue = diff < 0 ? absValueChange : 0;

      // Adjust inventory
      if (actual <= 0) {
        await client.query('DELETE FROM inventory WHERE id=$1', [inventory_id]);
      } else {
        const newValue = Math.round(Number(inv.value) * actual / recorded);
        await client.query('UPDATE inventory SET quantity=$1, value=$2 WHERE id=$3', [actual, newValue, inventory_id]);
      }

      // Stock history entry
      await writeHistory(client, {
        item_id, warehouse_id, quantity_change: diff, unit_name: inv.unit_name,
        type: 'SO', reference: `Stock Opname`, date: null,
        source_id: opname.id, source_type: 'opname',
        value: diff < 0 ? -absValueChange : absValueChange,
      });

      // Record waste to account
      if (wasteValue > 0) {
        await client.query('UPDATE accounts SET balance=balance-$1 WHERE id=$2', [wasteValue, wasteAcct.id]);
        totalWaste += wasteValue;
      }

      await client.query(
        `INSERT INTO stock_opname_items (opname_id,item_id,unit_index,unit_name,recorded_quantity,actual_quantity,difference,waste_value)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
        [opname.id, item_id, unit_index, inv.unit_name, recorded, actual, diff, wasteValue]
      );
    }

    const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(totalWaste);
    const { rows: [wh] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [warehouse_id]);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'opname', entity_id: opname.id, description: `Stock opname at ${wh.name} — waste: ${idrFmt}` });

    await client.query('COMMIT');
    res.status(201).json(opname);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── INVOICES ──────────────────────────────────────────────────────────────────

const invoiceListSelect = `
  SELECT inv.*, w.name AS warehouse_name, a.name AS account_name,
         b.name AS branch_name, dv.name AS division_name,
         v.name AS vendor_name,
         COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
  FROM invoices inv
  LEFT JOIN warehouses w  ON w.id  = inv.warehouse_id
  LEFT JOIN accounts a    ON a.id  = inv.account_id
  LEFT JOIN branches b    ON b.id  = inv.branch_id
  LEFT JOIN divisions dv  ON dv.id = inv.division_id
  LEFT JOIN vendors v     ON v.id  = inv.vendor_id
  LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
  GROUP BY inv.id, w.name, a.name, b.name, dv.name, v.name
`;

app.get('/api/invoices', async (req, res) => {
  const { status, type, search, date_from, date_to, page = 1, limit = 25 } = req.query;
  const params = [];
  const conditions = [];

  if (status && status !== 'all') {
    params.push(status);
    conditions.push(`inv.payment_status = $${params.length}`);
  }
  if (type && type !== 'all') {
    params.push(type);
    conditions.push(`inv.invoice_type = $${params.length}`);
  }
  if (date_from) { params.push(date_from); conditions.push(`inv.date >= $${params.length}`); }
  if (date_to)   { params.push(date_to);   conditions.push(`inv.date <= $${params.length}`); }
  if (search) {
    params.push(`%${search}%`);
    conditions.push(`(inv.invoice_number ILIKE $${params.length} OR inv.reference_number ILIKE $${params.length} OR v.name ILIKE $${params.length})`);
  }

  const where = conditions.length ? ' WHERE ' + conditions.join(' AND ') : '';
  const pageNum  = Math.max(1, parseInt(page) || 1);
  const pageSize = Math.min(100, Math.max(1, parseInt(limit) || 25));
  params.push(pageSize);
  const limitClause = `$${params.length}`;
  params.push((pageNum - 1) * pageSize);
  const offsetClause = `$${params.length}`;

  const sql = `
    WITH filtered AS (
      SELECT inv.*, w.name AS warehouse_name, a.name AS account_name,
             b.name AS branch_name, dv.name AS division_name,
             v.name AS vendor_name,
             COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
      FROM invoices inv
      LEFT JOIN warehouses w  ON w.id  = inv.warehouse_id
      LEFT JOIN accounts a    ON a.id  = inv.account_id
      LEFT JOIN branches b    ON b.id  = inv.branch_id
      LEFT JOIN divisions dv  ON dv.id = inv.division_id
      LEFT JOIN vendors v     ON v.id  = inv.vendor_id
      LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
      ${where}
      GROUP BY inv.id, w.name, a.name, b.name, dv.name, v.name
    )
    SELECT *, COUNT(*) OVER()::INT AS total_count
    FROM filtered
    ORDER BY created_at DESC
    LIMIT ${limitClause} OFFSET ${offsetClause}
  `;

  const [{ rows }, { rows: [outstanding] }] = await Promise.all([
    pool.query(sql, params),
    pool.query(`
      SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total,
             COUNT(DISTINCT inv.id)::INT AS count
      FROM invoices inv
      LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
      WHERE inv.payment_status IN ('unpaid', 'partial')
    `),
  ]);
  const total = rows[0]?.total_count ?? 0;
  res.json({
    invoices: rows, total, page: pageNum, limit: pageSize,
    outstanding_total: outstanding.total,
    outstanding_count: outstanding.count,
  });
});

app.get('/api/invoices/:id', async (req, res) => {
  const [invoiceRes, itemsRes] = await Promise.all([
    pool.query(invoiceListSelect + ' HAVING inv.id=$1 ORDER BY inv.created_at DESC', [req.params.id]),
    pool.query(
      `SELECT ii.*, i.name AS item_name, i.code AS item_code, i.units, v.name AS vendor_name
       FROM invoice_items ii
       LEFT JOIN items i ON i.id = ii.item_id
       LEFT JOIN vendors v ON v.id = ii.vendor_id
       WHERE ii.invoice_id=$1 ORDER BY ii.id`,
      [req.params.id]
    ),
  ]);
  if (!invoiceRes.rows.length) return res.status(404).json({ error: 'Invoice not found' });
  res.json({ ...invoiceRes.rows[0], items: itemsRes.rows });
});

app.post('/api/invoices', async (req, res) => {
  const { date, warehouse_id, payment_status, account_id, invoice_type = 'purchase', branch_id, division_id, vendor_id, reference_number, due_date, items } = req.body;
  if (!payment_status || !Array.isArray(items) || !items.length) {
    return res.status(400).json({ error: 'Payment status and at least one item are required' });
  }
  if (invoice_type === 'purchase' && !warehouse_id) {
    return res.status(400).json({ error: 'Warehouse is required for purchase invoices' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const prefix = invoice_type === 'expense' ? 'EXP' : 'INV';
    const { rows: [{ num }] } = await client.query(
      `SELECT $1 || '-' || LPAD(nextval('invoice_seq')::TEXT, 4, '0') AS num`, [prefix]
    );
    const invoiceDate = date || 'today';
    const resolvedDueDate = payment_status === 'paid' ? invoiceDate : (due_date || null);
    const { rows: [invoice] } = await client.query(
      `INSERT INTO invoices (invoice_number, date, warehouse_id, payment_status, account_id, invoice_type, branch_id, division_id, vendor_id, reference_number, due_date)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING *`,
      [num, invoiceDate, warehouse_id || null, payment_status, account_id || null, invoice_type, branch_id || null, division_id || null, vendor_id || null, reference_number || null, resolvedDueDate]
    );

    for (const item of items) {
      if (invoice_type === 'expense') {
        // Expense: link to non-stock item (item_id) or fall back to free-form description
        if (item.item_id) {
          await client.query(
            `INSERT INTO invoice_items (invoice_id, item_id, unit_index, quantity, price)
             VALUES ($1, $2, $3, $4, $5)`,
            [invoice.id, item.item_id, item.unit_index ?? 0, item.quantity, item.price]
          );
        } else {
          await client.query(
            `INSERT INTO invoice_items (invoice_id, description, quantity, price)
             VALUES ($1, $2, $3, $4)`,
            [invoice.id, item.description || null, item.quantity, item.price]
          );
        }
      } else {
        // Purchase: update inventory and write stock history
        const { rows: [vendor] } = await client.query('SELECT name FROM vendors WHERE id=$1', [item.vendor_id]);
        await client.query(
          `INSERT INTO invoice_items (invoice_id, item_id, vendor_id, quantity, unit_index, price)
           VALUES ($1, $2, $3, $4, $5, $6)`,
          [invoice.id, item.item_id, item.vendor_id, item.quantity, item.unit_index, item.price]
        );
        const { rows: [{ units }] } = await client.query('SELECT units FROM items WHERE id=$1', [item.item_id]);
        const lowestIdx = units.length - 1;
        let lowestQty = Number(item.quantity);
        for (let i = Number(item.unit_index) + 1; i < units.length; i++) {
          lowestQty *= units[i].perPrev;
        }
        const lowestUnitName = units[lowestIdx].name;
        const lineValue = Math.round(Number(item.quantity) * Number(item.price));
        // FIFO: always create a new lot so each purchase is tracked separately
        await client.query(
          `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date) VALUES ($1,$2,$3,$4,$5,$6)`,
          [item.item_id, invoice.warehouse_id, lowestQty, lowestIdx, lineValue, invoiceDate]
        );
        await writeHistory(client, {
          item_id: item.item_id, warehouse_id: invoice.warehouse_id,
          quantity_change: lowestQty, unit_name: lowestUnitName,
          vendor: vendor?.name ?? null, type: 'invoice',
          reference: num, date: invoiceDate,
          source_id: invoice.id, source_type: 'invoice',
          value: lineValue,
        });
      }
    }

    const total = items.reduce((s, it) => s + Number(it.quantity) * Number(it.price), 0);
    if (payment_status !== 'unpaid' && account_id) {
      // Validate that the payment account is a cash account (under Kas dan Setara Kas / account_number 12000)
      const { rows: [payAcct] } = await client.query(
        `SELECT a.account_type, a.balance, p.account_number AS parent_number
         FROM accounts a LEFT JOIN accounts p ON p.id = a.parent_id
         WHERE a.id = $1`, [account_id]
      );
      if (!payAcct || payAcct.account_type !== 'asset' || payAcct.parent_number !== 11000) {
        await client.query('ROLLBACK');
        return res.status(400).json({ error: 'Akun pembayaran harus berupa akun kas (di bawah Kas dan Setara Kas)' });
      }
      if (Number(payAcct.balance) < total) {
        await client.query('ROLLBACK');
        const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });
        return res.status(400).json({ error: `Saldo akun tidak cukup. Saldo tersedia: ${idrFmt.format(payAcct.balance)}, dibutuhkan: ${idrFmt.format(total)}` });
      }
      // Paid immediately: deduct from cash account
      await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [total, account_id]);
    } else if (payment_status === 'unpaid') {
      // Unpaid: credit Accounts Payable (liability increases)
      const ap = await getApAccount(client);
      if (ap) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [total, ap.id]);
    }
    // Dr side: increase inventory account (purchase) or expense account (expense)
    if (invoice_type === 'purchase') {
      const invAcct = await getInventoryAccount(client, warehouse_id);
      if (invAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [total, invAcct.id]);
    } else if (invoice_type === 'expense') {
      const expAcct = await getExpenseAccount(client, division_id, branch_id);
      if (expAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [total, expAcct.id]);
    }
    const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(total);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'invoice', entity_id: invoice.id, description: `Created ${invoice_type} invoice ${num} — ${items.length} item(s), total ${idrFmt}` });
    await client.query('COMMIT');
    res.status(201).json(invoice);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/invoices/:id', async (req, res) => {
  const { date, warehouse_id, payment_status, account_id, branch_id, division_id, vendor_id, reference_number, due_date, items } = req.body;
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Fetch old invoice + its total to reverse any prior balance deduction
    const { rows: [old] } = await client.query(
      `SELECT inv.payment_status, inv.account_id, inv.invoice_type,
              inv.warehouse_id, inv.branch_id, inv.division_id, inv.dispatch_id,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
       FROM invoices inv
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       WHERE inv.id = $1
       GROUP BY inv.id`,
      [req.params.id]
    );
    if (!old) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Invoice not found' }); }

    // Reverse old accounting entry
    if (old.payment_status !== 'unpaid' && old.account_id) {
      // Was paid: restore cash
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [old.total, old.account_id]);
    } else if (old.payment_status === 'unpaid') {
      // Was unpaid: reverse AP credit
      const ap = await getApAccount(client);
      if (ap) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [old.total, ap.id]);
    }
    // Reverse old Dr entry (skip dispatch-generated invoices — dispatch handler owns those)
    if (!old.dispatch_id) {
      if (old.invoice_type === 'purchase') {
        const invAcct = await getInventoryAccount(client, old.warehouse_id);
        if (invAcct) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [old.total, invAcct.id]);
      } else if (old.invoice_type === 'expense') {
        const expAcct = await getExpenseAccount(client, old.division_id, old.branch_id);
        if (expAcct) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [old.total, expAcct.id]);
      }
    }

    const resolvedDueDate = payment_status === 'paid' ? (date || null) : (due_date || null);
    const { rows } = await client.query(
      `UPDATE invoices SET date=$1, warehouse_id=$2, payment_status=$3, account_id=$4, branch_id=$5, division_id=$6, vendor_id=$7, reference_number=$8, due_date=$9
       WHERE id=$10 RETURNING *`,
      [date, warehouse_id || null, payment_status, account_id || null, branch_id || null, division_id || null, vendor_id || null, reference_number || null, resolvedDueDate, req.params.id]
    );
    await client.query('DELETE FROM invoice_items WHERE invoice_id=$1', [req.params.id]);
    for (const item of items) {
      if (old.invoice_type === 'expense') {
        if (item.item_id) {
          await client.query(
            `INSERT INTO invoice_items (invoice_id, item_id, unit_index, quantity, price) VALUES ($1,$2,$3,$4,$5)`,
            [req.params.id, item.item_id, item.unit_index ?? 0, item.quantity, item.price]
          );
        } else {
          await client.query(
            `INSERT INTO invoice_items (invoice_id, description, quantity, price) VALUES ($1,$2,$3,$4)`,
            [req.params.id, item.description || null, item.quantity, item.price]
          );
        }
      } else {
        await client.query(
          `INSERT INTO invoice_items (invoice_id, item_id, vendor_id, quantity, unit_index, price)
           VALUES ($1, $2, $3, $4, $5, $6)`,
          [req.params.id, item.item_id, item.vendor_id, item.quantity, item.unit_index, item.price]
        );
      }
    }

    // Apply new accounting entry
    const newTotal = items.reduce((s, it) => s + Number(it.quantity) * Number(it.price), 0);
    if (payment_status !== 'unpaid' && account_id) {
      const { rows: [payAcct] } = await client.query(
        `SELECT a.account_type, a.balance, p.account_number AS parent_number
         FROM accounts a LEFT JOIN accounts p ON p.id = a.parent_id
         WHERE a.id = $1`, [account_id]
      );
      if (!payAcct || payAcct.account_type !== 'asset' || payAcct.parent_number !== 11000) {
        await client.query('ROLLBACK');
        return res.status(400).json({ error: 'Akun pembayaran harus berupa akun kas (di bawah Kas dan Setara Kas)' });
      }
      if (Number(payAcct.balance) < newTotal) {
        await client.query('ROLLBACK');
        const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });
        return res.status(400).json({ error: `Saldo akun tidak cukup. Saldo tersedia: ${idrFmt.format(payAcct.balance)}, dibutuhkan: ${idrFmt.format(newTotal)}` });
      }
      await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [newTotal, account_id]);
    } else if (payment_status === 'unpaid') {
      const ap = await getApAccount(client);
      if (ap) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [newTotal, ap.id]);
    }
    // Apply new Dr entry (skip dispatch-generated invoices)
    if (!old.dispatch_id) {
      if (old.invoice_type === 'purchase') {
        const invAcct = await getInventoryAccount(client, warehouse_id);
        if (invAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [newTotal, invAcct.id]);
      } else if (old.invoice_type === 'expense') {
        const expAcct = await getExpenseAccount(client, division_id, branch_id);
        if (expAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [newTotal, expAcct.id]);
      }
    }

    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'update', entity_type: 'invoice', entity_id: req.params.id, description: `Updated invoice ${rows[0].invoice_number}` });
    await client.query('COMMIT');
    res.json(rows[0]);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.delete('/api/invoices/:id', requireAdmin, async (req, res) => {
  const { rows: [inv] } = await pool.query(
    `SELECT inv.invoice_number, inv.payment_status, inv.account_id, inv.photo_path,
            inv.invoice_type, inv.warehouse_id, inv.branch_id, inv.division_id, inv.dispatch_id,
            COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
     FROM invoices inv
     LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
     WHERE inv.id = $1
     GROUP BY inv.id`,
    [req.params.id]
  );
  if (!inv) return res.status(404).json({ error: 'Invoice not found' });
  if (inv.photo_path) {
    const filePath = path.join(uploadsDir, path.basename(inv.photo_path));
    if (fs.existsSync(filePath)) fs.unlinkSync(filePath);
  }
  await pool.query('DELETE FROM invoices WHERE id=$1', [req.params.id]);
  if (inv.payment_status !== 'unpaid' && inv.account_id) {
    // Was paid: restore cash account
    await pool.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [inv.total, inv.account_id]);
  } else if (inv.payment_status === 'unpaid') {
    // Was unpaid: reverse AP credit
    const ap = await getApAccount(pool);
    if (ap) await pool.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [inv.total, ap.id]);
  }
  // Reverse Dr entry (skip dispatch-generated invoices)
  if (!inv.dispatch_id) {
    if (inv.invoice_type === 'purchase') {
      const invAcct = await getInventoryAccount(pool, inv.warehouse_id);
      if (invAcct) await pool.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [inv.total, invAcct.id]);
    } else if (inv.invoice_type === 'expense') {
      const expAcct = await getExpenseAccount(pool, inv.division_id, inv.branch_id);
      if (expAcct) await pool.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [inv.total, expAcct.id]);
    }
  }
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'delete', entity_type: 'invoice', entity_id: req.params.id, description: `Deleted invoice ${inv.invoice_number}` });
  res.status(204).send();
});

// ── INVOICE PAYMENT ───────────────────────────────────────────────────────────

app.post('/api/invoices/:id/pay', async (req, res) => {
  const { cash_account_id, amount } = req.body;
  if (!cash_account_id) return res.status(400).json({ error: 'cash_account_id is required' });

  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    const { rows: [inv] } = await client.query(
      `SELECT inv.*, COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
       FROM invoices inv
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       WHERE inv.id = $1 GROUP BY inv.id`,
      [req.params.id]
    );
    if (!inv) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Invoice not found' }); }
    if (inv.payment_status === 'paid') { await client.query('ROLLBACK'); return res.status(400).json({ error: 'Invoice is already fully paid' }); }

    const remaining = Number(inv.total) - Number(inv.amount_paid);
    const payAmount = amount ? Math.min(Number(amount), remaining) : remaining;
    if (payAmount <= 0) { await client.query('ROLLBACK'); return res.status(400).json({ error: 'Payment amount must be positive' }); }

    // Verify cash account exists and has sufficient balance
    const { rows: [cashAcct] } = await client.query('SELECT id, name, balance FROM accounts WHERE id=$1', [cash_account_id]);
    if (!cashAcct) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Cash account not found' }); }
    if (Number(cashAcct.balance) < payAmount) {
      await client.query('ROLLBACK');
      const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 });
      return res.status(400).json({ error: `Saldo akun "${cashAcct.name}" tidak cukup. Saldo tersedia: ${idrFmt.format(cashAcct.balance)}, dibutuhkan: ${idrFmt.format(payAmount)}` });
    }

    // Debit AP (liability decreases)
    const ap = await getApAccount(client);
    if (ap) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [payAmount, ap.id]);

    // Credit cash account (asset decreases)
    await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [payAmount, cash_account_id]);

    const newAmountPaid = Number(inv.amount_paid) + payAmount;
    const newStatus = newAmountPaid >= Number(inv.total) ? 'paid' : 'partial';
    const { rows: [updated] } = await client.query(
      `UPDATE invoices SET payment_status=$1, account_id=$2, amount_paid=$3 WHERE id=$4 RETURNING *`,
      [newStatus, cash_account_id, newAmountPaid, req.params.id]
    );

    const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(payAmount);
    await logActivity(client, {
      user_id: req.user.id, username: req.user.username, action: 'update',
      entity_type: 'invoice', entity_id: req.params.id,
      description: `Paid invoice ${inv.invoice_number} — ${idrFmt} via "${cashAcct.name}" → status: ${newStatus}`,
    });

    await client.query('COMMIT');
    res.json(updated);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── STOCK TRANSFERS ───────────────────────────────────────────────────────────

app.get('/api/stock-transfers', async (req, res) => {
  const { rows } = await pool.query(
    `SELECT st.*, i.name AS item_name, i.code AS item_code,
            fw.name AS from_warehouse_name, tw.name AS to_warehouse_name,
            u.username AS transferred_by_name
     FROM stock_transfers st
     JOIN items i ON i.id = st.item_id
     JOIN warehouses fw ON fw.id = st.from_warehouse_id
     JOIN warehouses tw ON tw.id = st.to_warehouse_id
     LEFT JOIN users u ON u.id = st.transferred_by
     ORDER BY st.transferred_at DESC`
  );
  res.json(rows);
});

app.post('/api/stock-transfers', async (req, res) => {
  const { from_warehouse_id, to_warehouse_id, notes, items } = req.body;
  if (!from_warehouse_id || !to_warehouse_id) {
    return res.status(400).json({ error: 'Source and destination warehouse are required' });
  }
  if (from_warehouse_id === to_warehouse_id) {
    return res.status(400).json({ error: 'Source and destination warehouse must be different' });
  }
  if (!Array.isArray(items) || !items.length) {
    return res.status(400).json({ error: 'At least one item is required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    const { rows: [fw] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [from_warehouse_id]);
    const { rows: [tw] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [to_warehouse_id]);
    const transferRecords = [];
    const groupId = randomUUID();

    for (const it of items) {
      const { item_id, quantity, unit_index } = it;
      const { rows: [item] } = await client.query('SELECT name, code, units FROM items WHERE id=$1', [item_id]);
      const units = item.units;
      const unit_name = units[Number(unit_index)]?.name ?? '';

      // Convert requested quantity to the lowest unit (inventory is always stored at lowestIdx)
      const lowestIdx = units.length - 1;
      let conversionFactor = 1;
      for (let i = Number(unit_index) + 1; i < units.length; i++) {
        conversionFactor *= units[i].perPrev;
      }
      const lowestQty = Number(quantity) * conversionFactor;

      // FIFO: consume oldest lots from source first at the lowest unit
      const { rows: srcLots } = await client.query(
        'SELECT id, quantity, value FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3 ORDER BY date ASC, id ASC',
        [item_id, from_warehouse_id, lowestIdx]
      );
      const totalAvailable = srcLots.reduce((s, l) => s + Number(l.quantity), 0);
      if (totalAvailable < lowestQty) {
        await client.query('ROLLBACK');
        const availableInUnit = conversionFactor > 1 ? (totalAvailable / conversionFactor) : totalAvailable;
        const displayUnit = conversionFactor > 1 ? unit_name : (units[lowestIdx]?.name ?? unit_name);
        return res.status(400).json({ error: `Insufficient stock for "${item.name}" in ${fw.name} (available: ${availableInUnit} ${displayUnit})` });
      }

      let remaining = lowestQty;
      let transferredValue = 0;
      for (const lot of srcLots) {
        if (remaining <= 0) break;
        const lotQty = Number(lot.quantity);
        const lotVal = Number(lot.value);
        const consume = Math.min(remaining, lotQty);
        const consumeValue = lotQty > 0 ? Math.round(lotVal * consume / lotQty) : 0;
        transferredValue += consumeValue;
        remaining -= consume;
        if (consume >= lotQty) {
          await client.query('DELETE FROM inventory WHERE id=$1', [lot.id]);
        } else {
          await client.query('UPDATE inventory SET quantity=quantity-$1, value=value-$2 WHERE id=$3', [consume, consumeValue, lot.id]);
        }
      }

      // Create a new lot at destination at the lowest unit
      await client.query(
        `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date) VALUES ($1,$2,$3,$4,$5,CURRENT_DATE)`,
        [item_id, to_warehouse_id, lowestQty, lowestIdx, transferredValue]
      );

      await writeHistory(client, { item_id, warehouse_id: from_warehouse_id, quantity_change: -Number(quantity), unit_name, type: 'manual_out', reference: `Transfer → ${tw.name}`, date: null, source_id: groupId, source_type: 'transfer', value: -transferredValue });
      await writeHistory(client, { item_id, warehouse_id: to_warehouse_id,   quantity_change:  Number(quantity), unit_name, type: 'manual_in',  reference: `Transfer ← ${fw.name}`, date: null, source_id: groupId, source_type: 'transfer', value:  transferredValue });

      const { rows: [transfer] } = await client.query(
        `INSERT INTO stock_transfers (item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name, notes, transferred_by, group_id)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *`,
        [item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name, notes || null, req.user.id, groupId]
      );
      transferRecords.push({ ...transfer, item_name: item.name, unit_name, transferredValue });
    }

    // Cr source warehouse inventory account + Dr destination warehouse inventory account
    const totalTransferredValue = transferRecords.reduce((s, t) => s + t.transferredValue, 0);
    const srcInvAcct = await getInventoryAccount(client, from_warehouse_id);
    if (srcInvAcct) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [totalTransferredValue, srcInvAcct.id]);
    const dstInvAcct = await getInventoryAccount(client, to_warehouse_id);
    if (dstInvAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [totalTransferredValue, dstInvAcct.id]);

    const summary = transferRecords.map(t => `${t.quantity} ${t.unit_name} of "${t.item_name}"`).join(', ');
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'transfer', entity_type: 'transfer', entity_id: transferRecords[0].id, description: `Transferred from ${fw.name} to ${tw.name}: ${summary}` });

    await client.query('COMMIT');
    res.status(201).json(transferRecords);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── ACTIVITY LOG ──────────────────────────────────────────────────────────────

app.get('/api/activity-log', async (req, res) => {
  const page      = Math.max(1, parseInt(req.query.page)  || 1);
  const limit     = Math.min(100, Math.max(1, parseInt(req.query.limit) || 50));
  const offset    = (page - 1) * limit;
  const dateFrom  = req.query.date_from || null;
  const dateTo    = req.query.date_to   || null;
  const action    = req.query.action    || null;
  const entityType = req.query.entity_type || null;
  const search    = req.query.search    || null;

  const conditions = [];
  const params = [];

  if (dateFrom)   { params.push(dateFrom);   conditions.push(`created_at >= $${params.length}::date`); }
  if (dateTo)     { params.push(dateTo);     conditions.push(`created_at < ($${params.length}::date + interval '1 day')`); }
  if (action)     { params.push(action);     conditions.push(`action = $${params.length}`); }
  if (entityType) { params.push(entityType); conditions.push(`entity_type = $${params.length}`); }
  if (search)     { params.push(`%${search}%`); conditions.push(`(description ILIKE $${params.length} OR username ILIKE $${params.length})`); }

  const where = conditions.length ? `WHERE ${conditions.join(' AND ')}` : '';

  const countRes = await pool.query(`SELECT COUNT(*) FROM activity_log ${where}`, params);
  const total = Number(countRes.rows[0].count);

  params.push(limit, offset);
  const { rows } = await pool.query(
    `SELECT * FROM activity_log ${where} ORDER BY created_at DESC LIMIT $${params.length - 1} OFFSET $${params.length}`,
    params
  );

  res.json({ rows, total, page, limit });
});

app.get('/api/activity-log/export', async (req, res) => {
  const dateFrom   = req.query.date_from   || null;
  const dateTo     = req.query.date_to     || null;
  const action     = req.query.action      || null;
  const entityType = req.query.entity_type || null;
  const search     = req.query.search      || null;

  const conditions = [];
  const params = [];
  if (dateFrom)   { params.push(dateFrom);        conditions.push(`created_at >= $${params.length}::date`); }
  if (dateTo)     { params.push(dateTo);          conditions.push(`created_at < ($${params.length}::date + interval '1 day')`); }
  if (action)     { params.push(action);          conditions.push(`action = $${params.length}`); }
  if (entityType) { params.push(entityType);      conditions.push(`entity_type = $${params.length}`); }
  if (search)     { params.push(`%${search}%`);   conditions.push(`(description ILIKE $${params.length} OR username ILIKE $${params.length})`); }

  const where = conditions.length ? `WHERE ${conditions.join(' AND ')}` : '';
  const { rows } = await pool.query(
    `SELECT created_at, username, action, entity_type, description FROM activity_log ${where} ORDER BY created_at DESC`,
    params
  );

  const escape = (v) => `"${String(v ?? '').replace(/"/g, '""')}"`;
  const header = ['Waktu', 'Pengguna', 'Aksi', 'Tipe', 'Deskripsi'].map(escape).join(',');
  const lines  = rows.map(r => [
    escape(new Date(r.created_at).toLocaleString('id-ID')),
    escape(r.username),
    escape(r.action),
    escape(r.entity_type),
    escape(r.description),
  ].join(','));

  const csv = [header, ...lines].join('\r\n');
  const filename = `activity-log-${new Date().toISOString().split('T')[0]}.csv`;
  res.setHeader('Content-Type', 'text/csv; charset=utf-8');
  res.setHeader('Content-Disposition', `attachment; filename="${filename}"`);
  res.send('﻿' + csv); // BOM for Excel UTF-8 compatibility
});

app.delete('/api/activity-log', requireAdmin, async (req, res) => {
  const { before_date } = req.body;
  if (!before_date) return res.status(400).json({ error: 'before_date is required' });
  const { rowCount } = await pool.query(
    `DELETE FROM activity_log WHERE created_at < ($1::date + interval '1 day')`,
    [before_date]
  );
  res.json({ deleted: rowCount });
});

// ── BRANCHES ──────────────────────────────────────────────────────────────────

app.get('/api/branches', async (req, res) => {
  const { rows } = await pool.query(`
    SELECT b.*,
           ra.account_number AS revenue_account_number, ra.name AS revenue_account_name,
           ea.account_number AS expense_account_number, ea.name AS expense_account_name
    FROM branches b
    LEFT JOIN accounts ra ON ra.id = b.revenue_account_id
    LEFT JOIN accounts ea ON ea.id = b.expense_account_id
    ORDER BY b.name
  `);
  res.json(rows);
});

// Helper: create division + its revenue/expense/discount accounts under a branch.
// Must be called inside an existing transaction (client already BEGIN'd).
async function createDivisionWithAccounts(client, branchId, branchName, branchRevAccount, branchExpAccount, divName) {
  const { rows: [division] } = await client.query(
    'INSERT INTO divisions (branch_id, name) VALUES ($1, $2) RETURNING *',
    [branchId, divName]
  );

  if (branchRevAccount) {
    const base = branchRevAccount.account_number;
    const { rows: [{ next_rev }] } = await client.query(`
      SELECT COALESCE(MAX(account_number), $1) + 100 AS next_rev
      FROM accounts WHERE account_number > $1 AND account_number < $2 AND account_number % 100 = 0
    `, [base, base + 1000]);
    if (next_rev < base + 1000) {
      const { rows: [ra] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'revenue', 0, $3) RETURNING *`,
        [next_rev, `Pendapatan - ${branchName} / ${divName}`, branchRevAccount.id]
      );
      await client.query('UPDATE divisions SET revenue_account_id=$1 WHERE id=$2', [ra.id, division.id]);
      const discNum = next_rev + 1;
      const { rows: [da] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'revenue', 0, $3) RETURNING *`,
        [discNum, `Diskon - ${branchName} / ${divName}`, branchRevAccount.id]
      );
      await client.query('UPDATE divisions SET discount_account_id=$1 WHERE id=$2', [da.id, division.id]);
    }
  }

  if (branchExpAccount) {
    const base = branchExpAccount.account_number;
    const { rows: [{ next_exp }] } = await client.query(`
      SELECT COALESCE(MAX(account_number), $1) + 100 AS next_exp
      FROM accounts WHERE account_number > $1 AND account_number < $2 AND account_number % 100 = 0
    `, [base, base + 1000]);
    if (next_exp < base + 1000) {
      const { rows: [ea] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'expense', 0, $3) RETURNING *`,
        [next_exp, `Beban - ${branchName} / ${divName}`, branchExpAccount.id]
      );
      await client.query('UPDATE divisions SET expense_account_id=$1 WHERE id=$2', [ea.id, division.id]);
    }
  }

  return division;
}

app.post('/api/branches', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });

  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    const [{ rows: [revRoot] }, { rows: [expRoot] }] = await Promise.all([
      client.query(`SELECT id FROM accounts WHERE account_number = 40000 AND is_system = true LIMIT 1`),
      client.query(`SELECT id FROM accounts WHERE account_number = 50000 AND is_system = true LIMIT 1`),
    ]);

    // Next available branch-level revenue account: 41000, 42000, ...
    const { rows: [{ next_rev }] } = await client.query(`
      SELECT COALESCE(MAX(account_number), 40000) + 1000 AS next_rev
      FROM accounts WHERE account_number >= 41000 AND account_number < 50000 AND account_number % 1000 = 0
    `);
    const { rows: [{ next_exp }] } = await client.query(`
      SELECT COALESCE(MAX(account_number), 50000) + 1000 AS next_exp
      FROM accounts WHERE account_number >= 51000 AND account_number < 60000 AND account_number % 1000 = 0
    `);

    const { rows: [branch] } = await client.query(
      'INSERT INTO branches (name) VALUES ($1) RETURNING *', [name]
    );

    let revAccount = null, expAccount = null;
    if (revRoot && next_rev < 50000) {
      const { rows: [ra] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'revenue', 0, $3) RETURNING *`,
        [next_rev, `Pendapatan - ${name}`, revRoot.id]
      );
      revAccount = ra;
      await client.query('UPDATE branches SET revenue_account_id=$1 WHERE id=$2', [ra.id, branch.id]);
    }
    if (expRoot && next_exp < 60000) {
      const { rows: [ea] } = await client.query(
        `INSERT INTO accounts (account_number, name, account_type, balance, parent_id)
         VALUES ($1, $2, 'expense', 0, $3) RETURNING *`,
        [next_exp, `Beban - ${name}`, expRoot.id]
      );
      expAccount = ea;
      await client.query('UPDATE branches SET expense_account_id=$1 WHERE id=$2', [ea.id, branch.id]);
    }

    await client.query('COMMIT');
    res.status(201).json({ ...branch, revenue_account: revAccount, expense_account: expAccount });
  } catch (e) {
    await client.query('ROLLBACK');
    if (e.code === '23505') return res.status(409).json({ error: 'Branch name already exists' });
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/branches/:id', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  try {
    const { rows } = await pool.query('UPDATE branches SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
    if (!rows.length) return res.status(404).json({ error: 'Branch not found' });
    res.json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Branch name already exists' });
    throw e;
  }
});

app.delete('/api/branches/:id', requireAdmin, async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM dispatches WHERE branch_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Branch has existing dispatches and cannot be deleted' });
  const { rowCount } = await pool.query('DELETE FROM branches WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Branch not found' });
  res.status(204).end();
});

// ── DIVISIONS ─────────────────────────────────────────────────────────────────

app.get('/api/divisions', async (req, res) => {
  const { branch_id } = req.query;
  const params = [];
  let where = '';
  if (branch_id) { params.push(branch_id); where = 'WHERE d.branch_id=$1'; }
  const { rows } = await pool.query(
    `SELECT d.*, b.name AS branch_name,
            ra.account_number AS revenue_account_number, ra.name AS revenue_account_name,
            ea.account_number AS expense_account_number, ea.name AS expense_account_name,
            da.account_number AS discount_account_number, da.name AS discount_account_name
     FROM divisions d
     JOIN branches b ON b.id = d.branch_id
     LEFT JOIN accounts ra ON ra.id = d.revenue_account_id
     LEFT JOIN accounts ea ON ea.id = d.expense_account_id
     LEFT JOIN accounts da ON da.id = d.discount_account_id
     ${where} ORDER BY b.name, d.name`,
    params
  );
  res.json(rows);
});

app.post('/api/divisions', requireAdmin, async (req, res) => {
  const { branch_id, name } = req.body;
  if (!branch_id || !name) return res.status(400).json({ error: 'branch_id and name are required' });

  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    const { rows: [branch] } = await client.query('SELECT * FROM branches WHERE id=$1', [branch_id]);
    if (!branch) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Branch not found' }); }

    const branchRevAccount = branch.revenue_account_id
      ? (await client.query('SELECT id, account_number FROM accounts WHERE id=$1', [branch.revenue_account_id])).rows[0] || null
      : null;
    const branchExpAccount = branch.expense_account_id
      ? (await client.query('SELECT id, account_number FROM accounts WHERE id=$1', [branch.expense_account_id])).rows[0] || null
      : null;

    const division = await createDivisionWithAccounts(client, branch_id, branch.name, branchRevAccount, branchExpAccount, name);

    await client.query('COMMIT');
    res.status(201).json(division);
  } catch (e) {
    await client.query('ROLLBACK');
    if (e.code === '23505') return res.status(409).json({ error: 'Division name already exists in this branch' });
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/divisions/:id', requireAdmin, async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  try {
    const { rows } = await pool.query('UPDATE divisions SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
    if (!rows.length) return res.status(404).json({ error: 'Division not found' });
    res.json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Division name already exists in this branch' });
    throw e;
  }
});

app.delete('/api/divisions/:id', requireAdmin, async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM dispatches WHERE division_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Division has existing dispatches and cannot be deleted' });
  const { rowCount } = await pool.query('DELETE FROM divisions WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Division not found' });
  res.status(204).end();
});

// ── DIVISION CATEGORIES ───────────────────────────────────────────────────────

app.get('/api/division-categories', async (req, res) => {
  const { division_id } = req.query;
  const params = [];
  let where = '';
  if (division_id) { params.push(division_id); where = 'WHERE division_id=$1'; }
  const { rows } = await pool.query(
    `SELECT * FROM division_categories ${where} ORDER BY name`,
    params
  );
  res.json(rows);
});

app.post('/api/division-categories', requireAdmin, async (req, res) => {
  const { division_id, name } = req.body;
  if (!division_id || !name) return res.status(400).json({ error: 'division_id and name are required' });
  try {
    const { rows } = await pool.query(
      'INSERT INTO division_categories (division_id, name) VALUES ($1, $2) RETURNING *',
      [division_id, name]
    );
    res.status(201).json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Category already exists for this division' });
    throw e;
  }
});

app.delete('/api/division-categories/:id', requireAdmin, async (req, res) => {
  const { rowCount } = await pool.query('DELETE FROM division_categories WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Category not found' });
  res.status(204).end();
});

// ── DISPATCHES ────────────────────────────────────────────────────────────────

app.get('/api/dispatches', async (req, res) => {
  const { rows: dispatches } = await pool.query(
    `SELECT d.*, b.name AS branch_name, dv.name AS division_name,
            w.name AS warehouse_name, u.username AS dispatched_by_name
     FROM dispatches d
     JOIN branches b ON b.id = d.branch_id
     JOIN divisions dv ON dv.id = d.division_id
     JOIN warehouses w ON w.id = d.warehouse_id
     LEFT JOIN users u ON u.id = d.dispatched_by
     ORDER BY d.dispatched_at DESC`
  );
  const { rows: items } = await pool.query(
    `SELECT di.*, i.name AS item_name, i.code AS item_code
     FROM dispatch_items di
     JOIN items i ON i.id = di.item_id`
  );
  const itemsByDispatch = {};
  for (const item of items) {
    if (!itemsByDispatch[item.dispatch_id]) itemsByDispatch[item.dispatch_id] = [];
    itemsByDispatch[item.dispatch_id].push(item);
  }
  res.json(dispatches.map(d => ({ ...d, items: itemsByDispatch[d.id] || [] })));
});

app.post('/api/dispatches', async (req, res) => {
  const { branch_id, division_id, warehouse_id, notes, items } = req.body;
  if (!branch_id || !division_id || !warehouse_id || !Array.isArray(items) || !items.length) {
    return res.status(400).json({ error: 'Branch, division, warehouse, and at least one item are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    const { rows: [branch] }   = await client.query('SELECT name FROM branches WHERE id=$1', [branch_id]);
    const { rows: [division] } = await client.query('SELECT name FROM divisions WHERE id=$1', [division_id]);
    const { rows: [wh] }       = await client.query('SELECT name FROM warehouses WHERE id=$1', [warehouse_id]);

    const { rows: [dispatch] } = await client.query(
      `INSERT INTO dispatches (branch_id, division_id, warehouse_id, notes, dispatched_by)
       VALUES ($1,$2,$3,$4,$5) RETURNING *`,
      [branch_id, division_id, warehouse_id, notes || null, req.user.id]
    );

    const processedItems = [];
    for (const it of items) {
      const { item_id, quantity, unit_index } = it;
      const { rows: [item] } = await client.query('SELECT name, units FROM items WHERE id=$1', [item_id]);
      const units = item.units;
      const unit_name = units[Number(unit_index)]?.name ?? '';

      // Convert requested quantity to the lowest unit (inventory is always stored at lowestIdx)
      const lowestIdx = units.length - 1;
      let conversionFactor = 1;
      for (let i = Number(unit_index) + 1; i < units.length; i++) {
        conversionFactor *= units[i].perPrev;
      }
      const lowestQty = Number(quantity) * conversionFactor;

      // FIFO: get all lots oldest-first at the lowest unit, consume sequentially
      const { rows: lots } = await client.query(
        'SELECT id, quantity, value FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3 ORDER BY date ASC, id ASC',
        [item_id, warehouse_id, lowestIdx]
      );
      const totalAvailable = lots.reduce((s, l) => s + Number(l.quantity), 0);
      if (totalAvailable < lowestQty) {
        await client.query('ROLLBACK');
        const availableInUnit = conversionFactor > 1 ? (totalAvailable / conversionFactor) : totalAvailable;
        const displayUnit = conversionFactor > 1 ? unit_name : (units[lowestIdx]?.name ?? unit_name);
        return res.status(400).json({ error: `Insufficient stock for "${item.name}" in ${wh.name} (available: ${availableInUnit} ${displayUnit})` });
      }

      let remaining = lowestQty;
      let dispatchedValue = 0;
      for (const lot of lots) {
        if (remaining <= 0) break;
        const lotQty = Number(lot.quantity);
        const lotVal = Number(lot.value);
        const consume = Math.min(remaining, lotQty);
        const consumeValue = lotQty > 0 ? Math.round(lotVal * consume / lotQty) : 0;
        dispatchedValue += consumeValue;
        remaining -= consume;
        if (consume >= lotQty) {
          await client.query('DELETE FROM inventory WHERE id=$1', [lot.id]);
        } else {
          await client.query('UPDATE inventory SET quantity=quantity-$1, value=value-$2 WHERE id=$3', [consume, consumeValue, lot.id]);
        }
      }

      await client.query(
        'INSERT INTO dispatch_items (dispatch_id, item_id, quantity, unit_index, unit_name) VALUES ($1,$2,$3,$4,$5)',
        [dispatch.id, item_id, quantity, unit_index, unit_name]
      );

      await writeHistory(client, {
        item_id, warehouse_id, quantity_change: -Number(quantity), unit_name, type: 'pemakaian',
        reference: `Dispatch → ${branch.name} / ${division.name}`, date: null,
        source_id: dispatch.id, source_type: 'dispatch',
        value: -dispatchedValue,
      });

      processedItems.push({ item_name: item.name, quantity: Number(quantity), unit_name, dispatchedValue });
    }

    // Cr warehouse inventory account + Dr branch/division expense account
    const totalDispatchedValue = processedItems.reduce((s, pi) => s + pi.dispatchedValue, 0);
    const dispatchInvAcct = await getInventoryAccount(client, warehouse_id);
    if (dispatchInvAcct) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [totalDispatchedValue, dispatchInvAcct.id]);
    const dispatchExpAcct = await getExpenseAccount(client, division_id, branch_id);
    if (dispatchExpAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [totalDispatchedValue, dispatchExpAcct.id]);

    // Auto-create expense invoice so the dispatch appears in the expense report
    const { rows: [{ num }] } = await client.query(
      `SELECT 'EXP-' || LPAD(nextval('invoice_seq')::TEXT, 4, '0') AS num`
    );
    const { rows: [expInv] } = await client.query(
      `INSERT INTO invoices (invoice_number, date, payment_status, invoice_type, branch_id, division_id, dispatch_id)
       VALUES ($1, CURRENT_DATE, 'paid', 'expense', $2, $3, $4) RETURNING *`,
      [num, branch_id, division_id, dispatch.id]
    );
    for (const pi of processedItems) {
      const pricePerUnit = pi.quantity > 0 ? Math.round(pi.dispatchedValue / pi.quantity) : 0;
      await client.query(
        `INSERT INTO invoice_items (invoice_id, description, quantity, price) VALUES ($1,$2,$3,$4)`,
        [expInv.id, pi.item_name, pi.quantity, pricePerUnit]
      );
    }

    const summary = items.length === 1 ? `1 item` : `${items.length} items`;
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'dispatch', entity_id: dispatch.id, description: `Dispatched ${summary} from ${wh.name} to ${branch.name} / ${division.name}` });

    await client.query('COMMIT');
    res.status(201).json(dispatch);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── RECIPES ───────────────────────────────────────────────────────────────────

app.get('/api/recipes', async (req, res) => {
  const { rows: recipes } = await pool.query(
    `SELECT r.*, i.name AS output_item_name, i.units AS output_item_units
     FROM recipes r
     LEFT JOIN items i ON i.id = r.output_item_id
     ORDER BY r.name`
  );
  const { rows: ingredients } = await pool.query(
    `SELECT ri.*, i.name AS item_name, i.units AS item_units
     FROM recipe_ingredients ri
     JOIN items i ON i.id = ri.item_id
     ORDER BY ri.recipe_id, i.name`
  );
  const byRecipe = {};
  for (const ing of ingredients) {
    if (!byRecipe[ing.recipe_id]) byRecipe[ing.recipe_id] = [];
    byRecipe[ing.recipe_id].push(ing);
  }
  res.json(recipes.map(r => ({ ...r, ingredients: byRecipe[r.id] || [] })));
});

app.get('/api/recipes/:id', async (req, res) => {
  const { rows: [recipe] } = await pool.query(
    `SELECT r.*, i.name AS output_item_name, i.units AS output_item_units
     FROM recipes r
     LEFT JOIN items i ON i.id = r.output_item_id
     WHERE r.id = $1`,
    [req.params.id]
  );
  if (!recipe) return res.status(404).json({ error: 'Recipe not found' });
  const { rows: ingredients } = await pool.query(
    `SELECT ri.*, i.name AS item_name, i.units AS item_units
     FROM recipe_ingredients ri
     JOIN items i ON i.id = ri.item_id
     WHERE ri.recipe_id = $1
     ORDER BY i.name`,
    [req.params.id]
  );
  res.json({ ...recipe, ingredients });
});

app.post('/api/recipes', async (req, res) => {
  const { name, output_item_id, batch_size, batch_unit_index = 0, ingredients } = req.body;
  if (!name || !output_item_id || !batch_size || !Array.isArray(ingredients) || !ingredients.length) {
    return res.status(400).json({ error: 'Name, output item, batch size, and at least one ingredient are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [recipe] } = await client.query(
      `INSERT INTO recipes (name, output_item_id, batch_size, batch_unit_index) VALUES ($1,$2,$3,$4) RETURNING *`,
      [name, output_item_id, batch_size, batch_unit_index]
    );
    for (const ing of ingredients) {
      await client.query(
        `INSERT INTO recipe_ingredients (recipe_id, item_id, quantity, unit_index) VALUES ($1,$2,$3,$4)`,
        [recipe.id, ing.item_id, ing.quantity, ing.unit_index ?? 0]
      );
    }
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'recipe', entity_id: recipe.id, description: `Created recipe "${name}"` });
    await client.query('COMMIT');
    res.status(201).json(recipe);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.put('/api/recipes/:id', async (req, res) => {
  const { name, output_item_id, batch_size, batch_unit_index = 0, ingredients } = req.body;
  if (!name || !output_item_id || !batch_size || !Array.isArray(ingredients) || !ingredients.length) {
    return res.status(400).json({ error: 'Name, output item, batch size, and at least one ingredient are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [recipe] } = await client.query(
      `UPDATE recipes SET name=$1, output_item_id=$2, batch_size=$3, batch_unit_index=$4 WHERE id=$5 RETURNING *`,
      [name, output_item_id, batch_size, batch_unit_index, req.params.id]
    );
    if (!recipe) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Recipe not found' }); }
    await client.query('DELETE FROM recipe_ingredients WHERE recipe_id=$1', [req.params.id]);
    for (const ing of ingredients) {
      await client.query(
        `INSERT INTO recipe_ingredients (recipe_id, item_id, quantity, unit_index) VALUES ($1,$2,$3,$4)`,
        [req.params.id, ing.item_id, ing.quantity, ing.unit_index ?? 0]
      );
    }
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'update', entity_type: 'recipe', entity_id: req.params.id, description: `Updated recipe "${name}"` });
    await client.query('COMMIT');
    res.json(recipe);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.delete('/api/recipes/:id', async (req, res) => {
  const { rows: [recipe] } = await pool.query('SELECT name FROM recipes WHERE id=$1', [req.params.id]);
  if (!recipe) return res.status(404).json({ error: 'Recipe not found' });
  const inUse = await pool.query('SELECT 1 FROM productions WHERE recipe_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Recipe is used in production records and cannot be deleted' });
  await pool.query('DELETE FROM recipes WHERE id=$1', [req.params.id]);
  res.status(204).send();
});

// ── PRODUCTIONS ───────────────────────────────────────────────────────────────

app.get('/api/productions', async (req, res) => {
  const { rows } = await pool.query(
    `SELECT p.*, r.name AS recipe_name,
            oi.name AS output_item_name, oi.units AS output_item_units,
            w.name AS warehouse_name, u.username AS created_by_name
     FROM productions p
     JOIN recipes r ON r.id = p.recipe_id
     LEFT JOIN items oi ON oi.id = r.output_item_id
     JOIN warehouses w ON w.id = p.warehouse_id
     LEFT JOIN users u ON u.id = p.created_by
     ORDER BY p.created_at DESC`
  );
  res.json(rows);
});

app.post('/api/productions', async (req, res) => {
  const { recipe_id, warehouse_id, batches, date, notes } = req.body;
  if (!recipe_id || !warehouse_id || !batches || Number(batches) <= 0) {
    return res.status(400).json({ error: 'Recipe, warehouse, and number of batches are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Load recipe with ingredients
    const { rows: [recipe] } = await client.query(
      `SELECT r.*, i.name AS output_item_name, i.units AS output_item_units
       FROM recipes r
       LEFT JOIN items i ON i.id = r.output_item_id
       WHERE r.id = $1`,
      [recipe_id]
    );
    if (!recipe) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Recipe not found' }); }

    const { rows: ingredients } = await client.query(
      `SELECT ri.*, i.name AS item_name, i.units AS item_units
       FROM recipe_ingredients ri
       JOIN items i ON i.id = ri.item_id
       WHERE ri.recipe_id = $1`,
      [recipe_id]
    );

    const numBatches = Number(batches);
    const { rows: [wh] } = await client.query('SELECT name FROM warehouses WHERE id=$1', [warehouse_id]);

    // Phase 1: Check all ingredients have sufficient stock
    for (const ing of ingredients) {
      const units = ing.item_units;
      const lowestIdx = units.length - 1;
      let conversionFactor = 1;
      for (let i = Number(ing.unit_index) + 1; i < units.length; i++) {
        conversionFactor *= units[i].perPrev;
      }
      const neededLowest = Number(ing.quantity) * numBatches * conversionFactor;

      const { rows: lots } = await client.query(
        'SELECT quantity FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3',
        [ing.item_id, warehouse_id, lowestIdx]
      );
      const totalAvailable = lots.reduce((s, l) => s + Number(l.quantity), 0);
      if (totalAvailable < neededLowest) {
        await client.query('ROLLBACK');
        const availInUserUnit = conversionFactor > 1 ? (totalAvailable / conversionFactor) : totalAvailable;
        const displayUnit = units[Number(ing.unit_index)]?.name ?? units[lowestIdx]?.name ?? '';
        return res.status(400).json({
          error: `Insufficient stock for "${ing.item_name}" in ${wh.name} (need ${Number(ing.quantity) * numBatches} ${displayUnit}, available: ${availInUserUnit} ${displayUnit})`
        });
      }
    }

    // Phase 2: Deduct ingredients via FIFO
    for (const ing of ingredients) {
      const units = ing.item_units;
      const lowestIdx = units.length - 1;
      let conversionFactor = 1;
      for (let i = Number(ing.unit_index) + 1; i < units.length; i++) {
        conversionFactor *= units[i].perPrev;
      }
      const neededLowest = Number(ing.quantity) * numBatches * conversionFactor;
      const unit_name = units[Number(ing.unit_index)]?.name ?? '';

      const { rows: lots } = await client.query(
        'SELECT id, quantity, value FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3 ORDER BY date ASC, id ASC',
        [ing.item_id, warehouse_id, lowestIdx]
      );

      let remaining = neededLowest;
      for (const lot of lots) {
        if (remaining <= 0) break;
        const lotQty = Number(lot.quantity);
        const lotVal = Number(lot.value);
        const consume = Math.min(remaining, lotQty);
        const consumeValue = lotQty > 0 ? Math.round(lotVal * consume / lotQty) : 0;
        remaining -= consume;
        if (consume >= lotQty) {
          await client.query('DELETE FROM inventory WHERE id=$1', [lot.id]);
        } else {
          await client.query('UPDATE inventory SET quantity=quantity-$1, value=value-$2 WHERE id=$3', [consume, consumeValue, lot.id]);
        }
      }

      await writeHistory(client, {
        item_id: ing.item_id, warehouse_id,
        quantity_change: -(Number(ing.quantity) * numBatches),
        unit_name, type: 'production_out',
        reference: `Produksi: ${recipe.name} (${numBatches} batch)`,
        date: date || null,
      });
    }

    // Phase 3: Add output product to inventory
    const outUnits = recipe.output_item_units;
    const outLowestIdx = outUnits.length - 1;
    let outConversion = 1;
    for (let i = Number(recipe.batch_unit_index) + 1; i < outUnits.length; i++) {
      outConversion *= outUnits[i].perPrev;
    }
    const outputLowestQty = Number(recipe.batch_size) * numBatches * outConversion;
    const outputUnitName = outUnits[Number(recipe.batch_unit_index)]?.name ?? '';

    await client.query(
      `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date)
       VALUES ($1, $2, $3, $4, 0, $5)`,
      [recipe.output_item_id, warehouse_id, outputLowestQty, outLowestIdx, date || new Date().toISOString().split('T')[0]]
    );

    await writeHistory(client, {
      item_id: recipe.output_item_id, warehouse_id,
      quantity_change: Number(recipe.batch_size) * numBatches,
      unit_name: outputUnitName, type: 'production_in',
      reference: `Produksi: ${recipe.name} (${numBatches} batch)`,
      date: date || null,
    });

    // Phase 4: Save production record
    const outputQuantity = Number(recipe.batch_size) * numBatches;
    const { rows: [production] } = await client.query(
      `INSERT INTO productions (recipe_id, warehouse_id, batches, output_quantity, date, notes, created_by)
       VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *`,
      [recipe_id, warehouse_id, numBatches, outputQuantity, date || new Date().toISOString().split('T')[0], notes || null, req.user.id]
    );

    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'production', entity_id: production.id, description: `Produced ${outputQuantity} ${outputUnitName} of "${recipe.output_item_name}" (${numBatches} batch of ${recipe.name}) at ${wh.name}` });

    await client.query('COMMIT');
    res.status(201).json(production);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── POS IMPORT ────────────────────────────────────────────────────────────────

function parsePosXlsx(buffer) {
  const wb = XLSX.read(buffer, { type: 'buffer' });
  const ws = wb.Sheets[wb.SheetNames[0]];
  const range = XLSX.utils.decode_range(ws['!ref']);
  const getCell = (r, c) => { const cell = ws[XLSX.utils.encode_cell({ r, c })]; return cell ? cell.v : null; };

  // Find header row: column J (index 9) should contain "Kategori Produk"
  let headerRow = -1;
  for (let r = 0; r <= range.e.r; r++) {
    if (getCell(r, 9) === 'Kategori Produk') { headerRow = r; break; }
  }
  if (headerRow === -1) throw new Error('Format tidak dikenal: kolom J harus berisi "Kategori Produk"');

  const dataRows = [];
  const skippedRows = [];
  let currentAN     = null;
  let currentStatus = null;

  for (let r = headerRow + 1; r <= range.e.r; r++) {
    const rowA = getCell(r, 0);
    if (String(rowA) === 'TOTAL') break;

    const anVal = getCell(r, 39); // AN: Jenis Pembayaran
    if (anVal && anVal !== '-' && String(anVal).trim() !== '') currentAN = String(anVal).trim();

    const category = getCell(r, 9);  // J: Kategori Produk
    if (!category || String(category).trim() === '') continue;

    const gross       = Number(getCell(r, 20)) || 0; // U: Penjualan Kotor
    const disc        = Number(getCell(r, 21)) || 0; // V: Diskon
    const biaya       = Number(getCell(r, 29)) || 0; // AD: Biaya Tambahan
    const noPenjualan = getCell(r, 2);               // C: No Penjualan
    const dateRaw     = getCell(r, 3);               // D: Tanggal
    const product     = getCell(r, 11);              // L: Nama Produk

    // AV (index 47): Status — empty cells inherit from the previous row
    const statusRaw = getCell(r, 47);
    const statusVal = statusRaw ? String(statusRaw).trim() : '';
    if (statusVal !== '') currentStatus = statusVal;
    const statusStr = currentStatus || '';
    if (statusStr.toLowerCase() !== 'dibayar') {
      skippedRows.push({
        no_penjualan: noPenjualan ? String(noPenjualan).trim() : '',
        category:     String(category).trim(),
        product:      product ? String(product).trim() : '',
        gross,
        disc,
        biaya,
        net: gross - disc,
        payment: currentAN || '(tidak diketahui)',
        status:   statusStr || '(kosong)',
      });
      continue;
    }

    dataRows.push({
      no_penjualan: noPenjualan ? String(noPenjualan).trim() : '',
      category:     String(category).trim(),
      product:      product ? String(product).trim() : '',
      gross,
      disc,
      biaya,
      net: gross - disc,
      payment: currentAN || '(tidak diketahui)',
      dateRaw,
    });
  }

  // Resolve date: DD/MM/YYYY → YYYY-MM-DD
  const firstDate = dataRows.find(r => r.dateRaw)?.dateRaw;
  let saleDate = null;
  if (firstDate) {
    const s = String(firstDate);
    const parts = s.split('/');
    if (parts.length === 3) saleDate = `${parts[2]}-${parts[1].padStart(2,'0')}-${parts[0].padStart(2,'0')}`;
    else saleDate = s.split('T')[0];
  }

  // Aggregate by category
  const catMap = {};
  for (const row of dataRows) {
    if (!catMap[row.category]) catMap[row.category] = { name: row.category, gross: 0, disc: 0, net: 0 };
    catMap[row.category].gross += row.gross;
    catMap[row.category].disc  += row.disc;
    catMap[row.category].net   += row.net;
  }

  // Aggregate by category × payment (for breakdown)
  const catPayMap = {};
  for (const row of dataRows) {
    if (!catPayMap[row.category]) catPayMap[row.category] = {};
    if (!catPayMap[row.category][row.payment]) catPayMap[row.category][row.payment] = { gross: 0, disc: 0 };
    catPayMap[row.category][row.payment].gross += row.gross;
    catPayMap[row.category][row.payment].disc  += row.disc;
  }

  // Aggregate by payment method
  const payMap = {};
  for (const row of dataRows) {
    const k = row.payment;
    if (!payMap[k]) payMap[k] = { name: k, gross: 0, disc: 0, net: 0 };
    payMap[k].gross += row.gross;
    payMap[k].disc  += row.disc;
    payMap[k].net   += row.net;
  }

  const rows = dataRows.map(r => ({ no_penjualan: r.no_penjualan, category: r.category, product: r.product, gross: r.gross, disc: r.disc, biaya: r.biaya, net: r.net, payment: r.payment }));

  const categories = Object.values(catMap).map(c => ({
    ...c,
    byPayment: Object.entries(catPayMap[c.name] || {}).map(([payment, v]) => ({ payment, gross: v.gross, disc: v.disc })),
  }));

  // Rows that carry a biaya tambahan value — for the manual reference table
  const biayaRows = dataRows
    .filter(r => r.biaya > 0)
    .map(r => ({ no_penjualan: r.no_penjualan, category: r.category, product: r.product, biaya: r.biaya }));

  return {
    date: saleDate,
    categories,
    payments:      Object.values(payMap),
    rows,
    biayaRows,
    skippedRows,
    total:         dataRows.reduce((s, r) => s + r.net, 0),
    totalGross:    dataRows.reduce((s, r) => s + r.gross, 0),
    totalDisc:     dataRows.reduce((s, r) => s + r.disc, 0),
    totalBiaya:    dataRows.reduce((s, r) => s + r.biaya, 0),
  };
}

app.post('/api/pos-import/parse', memUpload.single('file'), async (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'Tidak ada file yang diunggah' });
  try {
    const result = parsePosXlsx(req.file.buffer);
    res.json({ ...result, filename: req.file.originalname });
  } catch (e) {
    res.status(400).json({ error: e.message });
  }
});

app.get('/api/pos-import', async (req, res) => {
  const { rows: imports } = await pool.query(
    `SELECT pi.*, u.username AS created_by_name
     FROM pos_imports pi
     LEFT JOIN users u ON u.id = pi.created_by
     ORDER BY pi.created_at DESC`
  );
  const { rows: lines } = await pool.query(
    `SELECT pil.*, a.name AS account_name
     FROM pos_import_lines pil
     LEFT JOIN accounts a ON a.id = pil.account_id
     ORDER BY pil.import_id, pil.line_type DESC, pil.amount DESC`
  );
  const byImport = {};
  for (const line of lines) {
    if (!byImport[line.import_id]) byImport[line.import_id] = [];
    byImport[line.import_id].push(line);
  }
  res.json(imports.map(i => ({ ...i, lines: byImport[i.id] || [] })));
});

app.post('/api/pos-import/confirm', async (req, res) => {
  const { date, description, filename, revenue_mappings, cash_mappings, discount_mappings = [], expense_mappings = [] } = req.body;
  // revenue_mappings:  [{label, account_id, amount (=net of discount and commission)}]
  // discount_mappings: [{label, account_id, amount (negative — reduces discount revenue account, informational)}]
  // expense_mappings:  [{label, account_id, amount (=biaya tambahan)}]
  // cash_mappings:     [{label, account_id, amount (=real received)}]
  // Balance: sum(revenue) = sum(cash) + sum(expense)  — discount is embedded in net revenue
  if (!date || !Array.isArray(revenue_mappings) || !Array.isArray(cash_mappings)) {
    return res.status(400).json({ error: 'Data tidak lengkap' });
  }
  const totalRevenue  = revenue_mappings.reduce((s, m) => s + Number(m.amount), 0);
  const totalCash     = cash_mappings.reduce((s, m) => s + Number(m.amount), 0);
  const totalDiscount = discount_mappings.reduce((s, m) => s + Number(m.amount), 0);
  const totalExpense  = expense_mappings.reduce((s, m) => s + Number(m.amount), 0);
  if (totalRevenue !== totalCash + totalExpense) {
    return res.status(400).json({ error: `Total tidak seimbang: pendapatan ${totalRevenue} ≠ kas ${totalCash} + biaya ${totalExpense}` });
  }
  for (const m of [...revenue_mappings, ...cash_mappings]) {
    if (!m.account_id) return res.status(400).json({ error: `Akun belum dipilih untuk "${m.label}"` });
    if (!m.amount || Number(m.amount) <= 0) return res.status(400).json({ error: `Jumlah tidak valid untuk "${m.label}"` });
  }
  for (const m of discount_mappings) {
    if (!m.account_id) return res.status(400).json({ error: `Akun diskon belum dipilih` });
  }
  for (const m of expense_mappings) {
    if (!m.account_id) return res.status(400).json({ error: `Akun biaya tambahan belum dipilih` });
  }

  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Credit revenue accounts (gross)
    for (const m of revenue_mappings) {
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(m.amount), m.account_id]);
    }
    // Discount amounts are negative — adding them reduces the revenue account balance
    for (const m of discount_mappings) {
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(m.amount), m.account_id]);
    }
    // Debit biaya tambahan expense accounts
    for (const m of expense_mappings) {
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(m.amount), m.account_id]);
    }
    // Debit (increase) cash/asset accounts (net)
    for (const m of cash_mappings) {
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(m.amount), m.account_id]);
    }

    // Save import record
    const { rows: [imp] } = await client.query(
      `INSERT INTO pos_imports (description, date, source_file, total_amount, created_by)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [description || `POS Import ${date}`, date, filename || null, totalRevenue, req.user.id]
    );

    for (const m of revenue_mappings) {
      await client.query(
        `INSERT INTO pos_import_lines (import_id, account_id, label, amount, line_type) VALUES ($1,$2,$3,$4,'revenue')`,
        [imp.id, m.account_id, m.label, Number(m.amount)]
      );
    }
    for (const m of discount_mappings) {
      await client.query(
        `INSERT INTO pos_import_lines (import_id, account_id, label, amount, line_type) VALUES ($1,$2,$3,$4,'discount')`,
        [imp.id, m.account_id, m.label, Number(m.amount)]
      );
    }
    for (const m of expense_mappings) {
      await client.query(
        `INSERT INTO pos_import_lines (import_id, account_id, label, amount, line_type) VALUES ($1,$2,$3,$4,'expense')`,
        [imp.id, m.account_id, m.label, Number(m.amount)]
      );
    }
    for (const m of cash_mappings) {
      await client.query(
        `INSERT INTO pos_import_lines (import_id, account_id, label, amount, line_type) VALUES ($1,$2,$3,$4,'cash')`,
        [imp.id, m.account_id, m.label, Number(m.amount)]
      );
    }

    const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(totalRevenue);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'pos_import', entity_id: imp.id, description: `POS Import ${date}: ${idrFmt} (${revenue_mappings.length} kategori, ${cash_mappings.length} metode bayar)` });

    await client.query('COMMIT');
    res.status(201).json(imp);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── SALES ─────────────────────────────────────────────────────────────────────

app.get('/api/sales', async (req, res) => {
  const { account_id } = req.query;
  const params = [];
  let where = '';
  if (account_id) { params.push(account_id); where = 'WHERE s.account_id = $1'; }
  const { rows } = await pool.query(
    `SELECT s.*, a.name AS account_name, u.username AS created_by_name,
            b.name AS branch_name, dv.name AS division_name
     FROM sales s
     JOIN accounts a ON a.id = s.account_id
     LEFT JOIN branches b ON b.id = s.branch_id
     LEFT JOIN divisions dv ON dv.id = s.division_id
     LEFT JOIN users u ON u.id = s.created_by
     ${where}
     ORDER BY s.date DESC, s.created_at DESC`,
    params
  );
  res.json(rows);
});

app.post('/api/sales', async (req, res) => {
  const { account_id, amount, description, date, branch_id, division_id } = req.body;
  if (!account_id || !amount || Number(amount) <= 0) {
    return res.status(400).json({ error: 'Account and a positive amount are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [sale] } = await client.query(
      `INSERT INTO sales (account_id, amount, description, date, created_by, branch_id, division_id)
       VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *`,
      [account_id, Number(amount), description || null, date || 'today', req.user.id, branch_id || null, division_id || null]
    );
    // Dr cash account (asset increases)
    await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(amount), account_id]);
    // Cr revenue account (revenue increases)
    const revAcct = await getRevenueAccount(client, division_id, branch_id);
    if (revAcct) await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(amount), revAcct.id]);
    const { rows: [acct] } = await client.query('SELECT name FROM accounts WHERE id=$1', [account_id]);
    const idrFmt = new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(amount);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'create', entity_type: 'sale', entity_id: sale.id, description: `Recorded sale of ${idrFmt} to account "${acct.name}"${description ? ': ' + description : ''}` });
    await client.query('COMMIT');
    res.status(201).json(sale);
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

app.delete('/api/sales/:id', async (req, res) => {
  const { rows: [sale] } = await pool.query(
    'SELECT s.*, a.name AS account_name FROM sales s JOIN accounts a ON a.id = s.account_id WHERE s.id=$1',
    [req.params.id]
  );
  if (!sale) return res.status(404).json({ error: 'Sale not found' });
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    await client.query('DELETE FROM sales WHERE id=$1', [req.params.id]);
    // Reverse Dr cash account
    await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [sale.amount, sale.account_id]);
    // Reverse Cr revenue account
    const revAcct = await getRevenueAccount(client, sale.division_id, sale.branch_id);
    if (revAcct) await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [sale.amount, revAcct.id]);
    await logActivity(client, { user_id: req.user.id, username: req.user.username, action: 'delete', entity_type: 'sale', entity_id: req.params.id, description: `Deleted sale record from account "${sale.account_name}"` });
    await client.query('COMMIT');
    res.status(204).end();
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── ACCOUNT ADJUSTMENTS ───────────────────────────────────────────────────────

app.get('/api/account-adjustments', async (req, res) => {
  const { account_id } = req.query;
  const params = [];
  let where = '';
  if (account_id) { params.push(account_id); where = 'WHERE aj.account_id = $1'; }
  const { rows } = await pool.query(
    `SELECT aj.*, a.name AS account_name, a.account_number, a.account_type
     FROM account_adjustments aj
     JOIN accounts a ON a.id = aj.account_id
     ${where}
     ORDER BY aj.created_at DESC
     LIMIT 500`,
    params
  );
  res.json(rows);
});

app.post('/api/account-adjustments', requireAdmin, async (req, res) => {
  const { account_id, amount, description } = req.body;
  if (!account_id || amount == null || !description?.trim()) {
    return res.status(400).json({ error: 'account_id, amount, and description are required' });
  }
  const amt = Math.round(Number(amount));
  if (!Number.isFinite(amt) || amt === 0) {
    return res.status(400).json({ error: 'amount must be a non-zero number' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [acct] } = await client.query('SELECT * FROM accounts WHERE id=$1', [account_id]);
    if (!acct) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Account not found' }); }
    const { rows: [adj] } = await client.query(
      `INSERT INTO account_adjustments (account_id, amount, description, created_by, created_by_name)
       VALUES ($1,$2,$3,$4,$5) RETURNING *`,
      [account_id, amt, description.trim(), req.user.id, req.user.username]
    );
    await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [amt, account_id]);
    await logActivity(client, {
      user_id: req.user.id, username: req.user.username,
      action: 'create', entity_type: 'account_adjustment', entity_id: adj.id,
      description: `Manual adjustment on "${acct.name}": ${amt > 0 ? '+' : ''}${amt}. Reason: ${description.trim()}`,
    });
    await client.query('COMMIT');
    res.status(201).json({ ...adj, account_name: acct.name, account_number: acct.account_number, account_type: acct.account_type });
  } catch (e) {
    await client.query('ROLLBACK');
    throw e;
  } finally {
    client.release();
  }
});

// ── FINANCIAL REPORT ──────────────────────────────────────────────────────────

app.get('/api/reports/financial', async (req, res) => {
  const { start_date, end_date } = req.query;
  const usePeriod = start_date && end_date;

  const { rows: accounts } = await pool.query(
    `SELECT id, account_number, name, account_type, balance, parent_id, is_system
     FROM accounts ORDER BY account_type, account_number NULLS LAST, name`
  );

  let periodMap = {};
  let adjMap = {};

  if (usePeriod) {
    // Compute period balances for revenue/expense accounts from transaction tables
    const { rows: periodRows } = await pool.query(`
      WITH
        sales_rev AS (
          SELECT COALESCE(dv.revenue_account_id, b.revenue_account_id) AS account_id,
                 SUM(s.amount) AS total
          FROM sales s
          LEFT JOIN divisions dv ON dv.id = s.division_id
          LEFT JOIN branches b ON b.id = s.branch_id
          WHERE s.date BETWEEN $1 AND $2
            AND COALESCE(dv.revenue_account_id, b.revenue_account_id) IS NOT NULL
          GROUP BY 1
        ),
        pos_rev AS (
          SELECT pil.account_id, SUM(pil.amount) AS total
          FROM pos_import_lines pil
          JOIN pos_imports pi ON pi.id = pil.import_id
          WHERE pil.line_type = 'revenue' AND pi.date BETWEEN $1 AND $2
          GROUP BY pil.account_id
        ),
        inv_exp AS (
          SELECT COALESCE(dv.expense_account_id, b.expense_account_id) AS account_id,
                 COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
          FROM invoices inv
          LEFT JOIN divisions dv ON dv.id = inv.division_id
          LEFT JOIN branches b ON b.id = inv.branch_id
          LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
          WHERE inv.invoice_type = 'expense' AND inv.date BETWEEN $1 AND $2
            AND COALESCE(dv.expense_account_id, b.expense_account_id) IS NOT NULL
          GROUP BY 1
        ),
        pos_exp AS (
          SELECT pil.account_id, SUM(pil.amount) AS total
          FROM pos_import_lines pil
          JOIN pos_imports pi ON pi.id = pil.import_id
          WHERE pil.line_type = 'expense' AND pi.date BETWEEN $1 AND $2
          GROUP BY pil.account_id
        ),
        pos_disc AS (
          SELECT pil.account_id, SUM(pil.amount) AS total
          FROM pos_import_lines pil
          JOIN pos_imports pi ON pi.id = pil.import_id
          WHERE pil.line_type = 'discount' AND pi.date BETWEEN $1 AND $2
          GROUP BY pil.account_id
        ),
        adj_period AS (
          SELECT account_id, SUM(amount) AS total
          FROM account_adjustments
          WHERE created_at::date BETWEEN $1 AND $2
          GROUP BY account_id
        ),
        combined AS (
          SELECT account_id, total FROM sales_rev
          UNION ALL SELECT account_id, total FROM pos_rev
          UNION ALL SELECT account_id, total FROM inv_exp
          UNION ALL SELECT account_id, total FROM pos_exp
          UNION ALL SELECT account_id, total FROM pos_disc
          UNION ALL SELECT account_id, total FROM adj_period
        )
      SELECT account_id, SUM(total)::BIGINT AS period_balance
      FROM combined GROUP BY account_id
    `, [start_date, end_date]);

    periodMap = Object.fromEntries(periodRows.map(r => [r.account_id, Number(r.period_balance)]));

    // Adjustments within period only (for the adjustments column)
    const { rows: adjRows } = await pool.query(
      `SELECT account_id, COALESCE(SUM(amount),0)::BIGINT AS total
       FROM account_adjustments WHERE created_at::date BETWEEN $1 AND $2
       GROUP BY account_id`,
      [start_date, end_date]
    );
    adjMap = Object.fromEntries(adjRows.map(r => [r.account_id, Number(r.total)]));
  } else {
    const { rows: adjRows } = await pool.query(
      `SELECT account_id, COALESCE(SUM(amount),0)::BIGINT AS total
       FROM account_adjustments GROUP BY account_id`
    );
    adjMap = Object.fromEntries(adjRows.map(r => [r.account_id, Number(r.total)]));
  }

  res.json(accounts.map(a => {
    const isIncomeStmt = a.account_type === 'revenue' || a.account_type === 'expense';
    const balance = usePeriod && isIncomeStmt
      ? (periodMap[a.id] || 0)
      : Number(a.balance);
    return { ...a, balance, total_adjustments: adjMap[a.id] || 0 };
  }));
});

// ── STATS ─────────────────────────────────────────────────────────────────────

app.get('/api/stats', async (req, res) => {
  try {
    const [itemsRes, invRes, valueRes, todayRes, outstandingRes, activityRes] = await Promise.all([
      pool.query('SELECT COUNT(*) FROM items'),
      pool.query('SELECT COUNT(*) FROM inventory'),
      pool.query('SELECT COALESCE(SUM(value), 0) AS total FROM inventory'),
      pool.query(`SELECT COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total, COUNT(DISTINCT inv.id)::INT AS count
                  FROM invoices inv
                  LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
                  WHERE inv.date = CURRENT_DATE AND inv.invoice_type = 'purchase'`),
      pool.query(`SELECT inv.id, inv.invoice_number, inv.amount_paid,
                         inv.payment_status, inv.due_date, inv.date, inv.invoice_type,
                         v.name AS vendor_name,
                         COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
                  FROM invoices inv
                  LEFT JOIN vendors v ON v.id = inv.vendor_id
                  LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
                  WHERE inv.payment_status IN ('unpaid','partial')
                  GROUP BY inv.id, v.name
                  ORDER BY MIN(inv.due_date) ASC NULLS LAST, MIN(inv.date) DESC`),
      pool.query(`SELECT id, user_id, username, action, entity_type, description, created_at
                  FROM activity_log ORDER BY created_at DESC LIMIT 5`),
    ]);
    res.json({
      totalItems:            Number(itemsRes.rows[0].count),
      totalInventoryRecords: Number(invRes.rows[0].count),
      totalInventoryValue:   Number(valueRes.rows[0].total),
      todayPurchasesTotal:   Number(todayRes.rows[0].total),
      todayPurchasesCount:   Number(todayRes.rows[0].count),
      outstandingInvoices:   outstandingRes.rows,
      recentActivity:        activityRes.rows,
    });
  } catch (err) {
    console.error('Stats error:', err);
    res.status(500).json({ error: err.message });
  }
});

// ── INVOICE PHOTO ─────────────────────────────────────────────────────────────

app.post('/api/invoices/:id/photo', upload.single('photo'), async (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'No file provided' });
  // Delete old photo file if one exists
  const { rows: [inv] } = await pool.query('SELECT photo_path FROM invoices WHERE id=$1', [req.params.id]);
  if (inv?.photo_path) {
    const oldFile = path.join(__dirname, inv.photo_path.replace(/^\//, ''));
    if (fs.existsSync(oldFile)) fs.unlinkSync(oldFile);
  }
  const photoPath = `/uploads/${req.file.filename}`;
  await pool.query('UPDATE invoices SET photo_path=$1 WHERE id=$2', [photoPath, req.params.id]);
  res.json({ photo_path: photoPath });
});

app.delete('/api/invoices/:id/photo', async (req, res) => {
  const { rows: [inv] } = await pool.query('SELECT photo_path FROM invoices WHERE id=$1', [req.params.id]);
  if (!inv) return res.status(404).json({ error: 'Invoice not found' });
  if (inv.photo_path) {
    const filePath = path.join(uploadsDir, path.basename(inv.photo_path));
    if (fs.existsSync(filePath)) fs.unlinkSync(filePath);
    await pool.query('UPDATE invoices SET photo_path=NULL WHERE id=$1', [req.params.id]);
  }
  res.status(204).end();
});

// ── DETAIL BY ID ──────────────────────────────────────────────────────────────

app.get('/api/stock-transfers/group/:groupId', async (req, res) => {
  const { rows } = await pool.query(
    `SELECT st.*, i.name AS item_name, i.code AS item_code,
            fw.name AS from_warehouse_name, tw.name AS to_warehouse_name,
            u.username AS transferred_by_name
     FROM stock_transfers st
     JOIN items i ON i.id = st.item_id
     JOIN warehouses fw ON fw.id = st.from_warehouse_id
     JOIN warehouses tw ON tw.id = st.to_warehouse_id
     LEFT JOIN users u ON u.id = st.transferred_by
     WHERE st.group_id = $1
     ORDER BY st.transferred_at`,
    [req.params.groupId]
  );
  if (!rows.length) return res.status(404).json({ error: 'Transfer group not found' });
  res.json(rows);
});

app.get('/api/dispatches/:id', async (req, res) => {
  const { rows: [dispatch] } = await pool.query(
    `SELECT d.*, b.name AS branch_name, dv.name AS division_name,
            w.name AS warehouse_name, u.username AS dispatched_by_name
     FROM dispatches d
     JOIN branches b ON b.id = d.branch_id
     JOIN divisions dv ON dv.id = d.division_id
     JOIN warehouses w ON w.id = d.warehouse_id
     LEFT JOIN users u ON u.id = d.dispatched_by
     WHERE d.id = $1`,
    [req.params.id]
  );
  if (!dispatch) return res.status(404).json({ error: 'Dispatch not found' });
  const { rows: items } = await pool.query(
    `SELECT di.*, i.name AS item_name, i.code AS item_code
     FROM dispatch_items di JOIN items i ON i.id = di.item_id
     WHERE di.dispatch_id = $1 ORDER BY di.id`,
    [req.params.id]
  );
  res.json({ ...dispatch, items });
});

app.get('/api/stock-opname/:id', async (req, res) => {
  const { rows: [opname] } = await pool.query(
    `SELECT so.*, w.name AS warehouse_name, u.username AS performed_by_name
     FROM stock_opname so
     JOIN warehouses w ON w.id = so.warehouse_id
     LEFT JOIN users u ON u.id = so.performed_by
     WHERE so.id = $1`,
    [req.params.id]
  );
  if (!opname) return res.status(404).json({ error: 'Opname not found' });
  const { rows: items } = await pool.query(
    `SELECT soi.*, i.name AS item_name, i.code AS item_code
     FROM stock_opname_items soi JOIN items i ON i.id = soi.item_id
     WHERE soi.opname_id = $1 ORDER BY soi.id`,
    [req.params.id]
  );
  res.json({ ...opname, items });
});

// ── INVENTORY VALUE REPORT ────────────────────────────────────────────────────

app.get('/api/reports/inventory-value', async (req, res) => {
  const { warehouse_id, date_from, date_to } = req.query;
  const params = [];
  const conditions = [];
  if (warehouse_id && warehouse_id !== 'all') {
    params.push(warehouse_id);
    conditions.push(`inv.warehouse_id = $${params.length}`);
  }
  if (date_from) { params.push(date_from); conditions.push(`inv.date >= $${params.length}`); }
  if (date_to)   { params.push(date_to);   conditions.push(`inv.date <= $${params.length}`); }
  const where = conditions.length ? ' WHERE ' + conditions.join(' AND ') : '';

  const { rows } = await pool.query(`
    SELECT
      w.id AS warehouse_id, w.name AS warehouse_name,
      COUNT(DISTINCT inv.item_id)::INT AS item_count,
      COALESCE(SUM(inv.value), 0)::BIGINT AS total_value,
      json_agg(json_build_object(
        'item_id',   inv.item_id,
        'item_name', i.name,
        'item_code', i.code,
        'quantity',  inv.quantity,
        'unit_name', i.units->inv.unit_index->>'name',
        'value',     inv.value,
        'date',      inv.date
      ) ORDER BY inv.value DESC NULLS LAST) AS items
    FROM inventory inv
    JOIN warehouses w ON w.id = inv.warehouse_id
    JOIN items i ON i.id = inv.item_id
    ${where}
    GROUP BY w.id, w.name
    ORDER BY total_value DESC
  `, params);
  res.json(rows);
});

// ── EXPENSE SUMMARY REPORT ────────────────────────────────────────────────────

app.get('/api/reports/expense-summary', async (req, res) => {
  const { date_from, date_to } = req.query;
  const conditions = [`inv.invoice_type = 'expense'`, `inv.branch_id IS NOT NULL`];
  const params = [];
  if (date_from) { params.push(date_from); conditions.push(`inv.date >= $${params.length}`); }
  if (date_to)   { params.push(date_to);   conditions.push(`inv.date <= $${params.length}`); }
  const where = `WHERE ${conditions.join(' AND ')}`;

  const [branchRes, divRes] = await Promise.all([
    pool.query(
      `SELECT b.id AS branch_id, b.name AS branch_name,
              COUNT(DISTINCT inv.id)::INT AS invoice_count,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
       FROM invoices inv
       JOIN branches b ON b.id = inv.branch_id
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       ${where}
       GROUP BY b.id, b.name
       ORDER BY total_amount DESC`,
      params
    ),
    pool.query(
      `SELECT b.id AS branch_id, b.name AS branch_name,
              dv.id AS division_id, dv.name AS division_name,
              COUNT(DISTINCT inv.id)::INT AS invoice_count,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
       FROM invoices inv
       JOIN branches b   ON b.id  = inv.branch_id
       JOIN divisions dv ON dv.id = inv.division_id
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       ${where}
       GROUP BY b.id, b.name, dv.id, dv.name
       ORDER BY b.name, total_amount DESC`,
      params
    ),
  ]);

  const divsByBranch = {};
  for (const d of divRes.rows) {
    if (!divsByBranch[d.branch_id]) divsByBranch[d.branch_id] = [];
    divsByBranch[d.branch_id].push(d);
  }
  const result = branchRes.rows.map(b => ({
    ...b,
    divisions: divsByBranch[b.branch_id] || [],
  }));
  res.json(result);
});

// ── EXPENSE REPORT ────────────────────────────────────────────────────────────

app.get('/api/expense-report', async (req, res) => {
  const { branch_id, division_id, date_from, date_to } = req.query;
  const conditions = [`inv.invoice_type = 'expense'`, `inv.branch_id IS NOT NULL`];
  const params = [];
  if (branch_id)   { params.push(branch_id);   conditions.push(`inv.branch_id = $${params.length}`); }
  if (division_id) { params.push(division_id); conditions.push(`inv.division_id = $${params.length}`); }
  if (date_from)   { params.push(date_from);   conditions.push(`inv.date >= $${params.length}`); }
  if (date_to)     { params.push(date_to);     conditions.push(`inv.date <= $${params.length}`); }
  const where = `WHERE ${conditions.join(' AND ')}`;

  const [summaryRes, invoiceRes, itemRes] = await Promise.all([
    pool.query(
      `SELECT b.id AS branch_id, b.name AS branch_name,
              dv.id AS division_id, dv.name AS division_name,
              COUNT(DISTINCT inv.id)::INT AS invoice_count,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_amount
       FROM invoices inv
       JOIN branches b   ON b.id  = inv.branch_id
       JOIN divisions dv ON dv.id = inv.division_id
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       ${where}
       GROUP BY b.id, b.name, dv.id, dv.name
       ORDER BY b.name, dv.name`,
      params
    ),
    pool.query(
      `SELECT inv.id, inv.invoice_number, inv.date, inv.payment_status,
              inv.branch_id, inv.division_id, inv.photo_path, inv.dispatch_id,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
       FROM invoices inv
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       ${where}
       GROUP BY inv.id
       ORDER BY inv.date DESC`,
      params
    ),
    pool.query(
      `SELECT inv.branch_id, inv.division_id,
              ii.item_id,
              COALESCE(it.name, ii.description) AS description,
              SUM(ii.quantity)::BIGINT AS total_qty,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total_value
       FROM invoices inv
       JOIN invoice_items ii ON ii.invoice_id = inv.id
       LEFT JOIN items it ON it.id = ii.item_id
       ${where}
       GROUP BY inv.branch_id, inv.division_id, ii.item_id, COALESCE(it.name, ii.description)
       ORDER BY inv.branch_id, inv.division_id, total_value DESC`,
      params
    ),
  ]);

  // Nest invoices and items inside each branch/division summary
  const invoicesByGroup = {};
  for (const inv of invoiceRes.rows) {
    const key = `${inv.branch_id}::${inv.division_id}`;
    if (!invoicesByGroup[key]) invoicesByGroup[key] = [];
    invoicesByGroup[key].push(inv);
  }
  const itemsByGroup = {};
  for (const it of itemRes.rows) {
    const key = `${it.branch_id}::${it.division_id}`;
    if (!itemsByGroup[key]) itemsByGroup[key] = [];
    itemsByGroup[key].push(it);
  }
  const result = summaryRes.rows.map(g => ({
    ...g,
    invoices: invoicesByGroup[`${g.branch_id}::${g.division_id}`] || [],
    item_usage: itemsByGroup[`${g.branch_id}::${g.division_id}`] || [],
  }));
  res.json(result);
});

app.listen(PORT, () => console.log(`Server running on http://localhost:${PORT}`));
