import express from 'express';
import cors from 'cors';
import pg from 'pg';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import { randomUUID } from 'crypto';
import multer from 'multer';
import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';

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

const { Pool } = pg;

const app = express();
const PORT = 5000;
const JWT_SECRET = process.env.JWT_SECRET || 'inventory_secret_change_in_prod';

app.use(cors());
app.use(express.json());
app.use('/uploads', express.static(uploadsDir));

const pool = new Pool({
  host: 'localhost',
  port: 5432,
  database: 'inventory_app',
  user: 'postgres',
  password: 'seesaw',
});

// ── AUTH ──────────────────────────────────────────────────────────────────────

app.post('/api/auth/login', async (req, res) => {
  const { username, password } = req.body;
  if (!username || !password) return res.status(400).json({ error: 'Username and password required' });
  const { rows } = await pool.query('SELECT * FROM users WHERE username=$1', [username]);
  const user = rows[0];
  if (!user) return res.status(401).json({ error: 'Invalid credentials' });
  const valid = await bcrypt.compare(password, user.password_hash);
  if (!valid) return res.status(401).json({ error: 'Invalid credentials' });
  const token = jwt.sign({ id: user.id, username: user.username, role: user.role }, JWT_SECRET, { expiresIn: '8h' });
  res.json({ token, user: { id: user.id, username: user.username, role: user.role } });
});

function requireAuth(req, res, next) {
  const header = req.headers.authorization;
  if (!header?.startsWith('Bearer ')) return res.status(401).json({ error: 'Unauthorized' });
  try {
    req.user = jwt.verify(header.slice(7), JWT_SECRET);
    next();
  } catch {
    res.status(401).json({ error: 'Invalid or expired token' });
  }
}

app.use('/api', (req, res, next) => {
  if (req.path === '/auth/login') return next();
  requireAuth(req, res, next);
});

// ── USERS ─────────────────────────────────────────────────────────────────────

app.get('/api/users', async (req, res) => {
  const { rows } = await pool.query('SELECT id, username, role, created_at FROM users ORDER BY created_at');
  res.json(rows);
});

app.post('/api/users', async (req, res) => {
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

app.put('/api/users/:id', async (req, res) => {
  const { username, password, role } = req.body;
  const fields = [];
  const vals = [];
  if (username) { fields.push(`username=$${fields.length+1}`); vals.push(username); }
  if (role)     { fields.push(`role=$${fields.length+1}`);     vals.push(role); }
  if (password) { fields.push(`password_hash=$${fields.length+1}`); vals.push(await bcrypt.hash(password, 10)); }
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

app.delete('/api/users/:id', async (req, res) => {
  if (req.user.id === req.params.id) return res.status(400).json({ error: 'Cannot delete your own account' });
  await pool.query('DELETE FROM users WHERE id=$1', [req.params.id]);
  res.status(204).end();
});

// ── WAREHOUSES ────────────────────────────────────────────────────────────────

app.get('/api/warehouses', async (req, res) => {
  const { rows } = await pool.query('SELECT * FROM warehouses ORDER BY name');
  res.json(rows);
});

app.post('/api/warehouses', async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('INSERT INTO warehouses (name) VALUES ($1) RETURNING *', [name]);
  res.status(201).json(rows[0]);
});

app.put('/api/warehouses/:id', async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('UPDATE warehouses SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Warehouse not found' });
  res.json(rows[0]);
});

app.delete('/api/warehouses/:id', async (req, res) => {
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

app.post('/api/vendors', async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('INSERT INTO vendors (name) VALUES ($1) RETURNING *', [name]);
  res.status(201).json(rows[0]);
});

app.put('/api/vendors/:id', async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query('UPDATE vendors SET name=$1 WHERE id=$2 RETURNING *', [name, req.params.id]);
  if (!rows.length) return res.status(404).json({ error: 'Vendor not found' });
  res.json(rows[0]);
});

app.delete('/api/vendors/:id', async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM invoice_items WHERE vendor_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Vendor is referenced by existing records' });
  const { rowCount } = await pool.query('DELETE FROM vendors WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Vendor not found' });
  res.status(204).send();
});

// ── ACCOUNTS ─────────────────────────────────────────────────────────────────

app.get('/api/accounts', async (req, res) => {
  const { rows } = await pool.query('SELECT * FROM accounts ORDER BY name');
  res.json(rows);
});

app.post('/api/accounts', async (req, res) => {
  const { name, balance = 0 } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query(
    'INSERT INTO accounts (name, balance) VALUES ($1, $2) RETURNING *',
    [name, Number(balance)]
  );
  res.status(201).json(rows[0]);
});

app.put('/api/accounts/:id', async (req, res) => {
  const { name, balance } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  const { rows } = await pool.query(
    'UPDATE accounts SET name=$1, balance=$2 WHERE id=$3 RETURNING *',
    [name, Number(balance ?? 0), req.params.id]
  );
  if (!rows.length) return res.status(404).json({ error: 'Account not found' });
  res.json(rows[0]);
});

app.delete('/api/accounts/:id', async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM invoices WHERE account_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Account is referenced by existing invoices' });
  const { rowCount } = await pool.query('DELETE FROM accounts WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Account not found' });
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

app.post('/api/items', async (req, res) => {
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

app.put('/api/items/:id', async (req, res) => {
  const { name, code, units, is_stock } = req.body;
  const { rows } = await pool.query(
    'UPDATE items SET name=$1, code=$2, units=$3, is_stock=COALESCE($4, is_stock) WHERE id=$5 RETURNING *',
    [name, code, JSON.stringify(units), is_stock ?? null, req.params.id]
  );
  if (!rows.length) return res.status(404).json({ error: 'Item not found' });
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'update', entity_type: 'item', entity_id: rows[0].id, description: `Updated item "${name}" (${code})` });
  res.json(rows[0]);
});

app.delete('/api/items/:id', async (req, res) => {
  const { rows: [item] } = await pool.query('SELECT name, code FROM items WHERE id=$1', [req.params.id]);
  if (!item) return res.status(404).json({ error: 'Item not found' });
  await pool.query('DELETE FROM items WHERE id=$1', [req.params.id]);
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
`).then(() => pool.query(`
  ALTER TABLE invoice_items ALTER COLUMN item_id       DROP NOT NULL;
  ALTER TABLE invoice_items ALTER COLUMN vendor_id     DROP NOT NULL;
  ALTER TABLE invoice_items ALTER COLUMN unit_index    DROP NOT NULL;
  ALTER TABLE invoices      ALTER COLUMN payment_method DROP NOT NULL;
  ALTER TABLE invoices      ALTER COLUMN warehouse_id   DROP NOT NULL;
`)).catch(() => {/* columns may already be nullable */});

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
  const { status } = req.query;
  const params = [];
  let having = '';
  if (status && status !== 'all') {
    params.push(status);
    having = ` HAVING inv.payment_status = $1`;
  }
  const { rows } = await pool.query(invoiceListSelect + having + ' ORDER BY inv.created_at DESC', params);
  res.json(rows);
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
  const { date, warehouse_id, payment_status, account_id, invoice_type = 'purchase', branch_id, division_id, vendor_id, reference_number, items } = req.body;
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
    const { rows: [invoice] } = await client.query(
      `INSERT INTO invoices (invoice_number, date, warehouse_id, payment_status, account_id, invoice_type, branch_id, division_id, vendor_id, reference_number)
       VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING *`,
      [num, invoiceDate, warehouse_id || null, payment_status, account_id || null, invoice_type, branch_id || null, division_id || null, vendor_id || null, reference_number || null]
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
        const { rows: existing } = await client.query(
          `SELECT id FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3`,
          [item.item_id, invoice.warehouse_id, lowestIdx]
        );
        if (existing.length) {
          await client.query('UPDATE inventory SET quantity=quantity+$1, value=value+$2 WHERE id=$3', [lowestQty, lineValue, existing[0].id]);
        } else {
          await client.query(
            `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date) VALUES ($1,$2,$3,$4,$5,$6)`,
            [item.item_id, invoice.warehouse_id, lowestQty, lowestIdx, lineValue, invoiceDate]
          );
        }
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
      await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [total, account_id]);
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
  const { date, warehouse_id, payment_status, account_id, branch_id, division_id, vendor_id, reference_number, items } = req.body;
  const client = await pool.connect();
  try {
    await client.query('BEGIN');

    // Fetch old invoice + its total to reverse any prior balance deduction
    const { rows: [old] } = await client.query(
      `SELECT inv.payment_status, inv.account_id, inv.invoice_type,
              COALESCE(SUM(ii.quantity * ii.price), 0)::BIGINT AS total
       FROM invoices inv
       LEFT JOIN invoice_items ii ON ii.invoice_id = inv.id
       WHERE inv.id = $1
       GROUP BY inv.id`,
      [req.params.id]
    );
    if (!old) { await client.query('ROLLBACK'); return res.status(404).json({ error: 'Invoice not found' }); }

    // Restore old deduction
    if (old.payment_status !== 'unpaid' && old.account_id) {
      await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [old.total, old.account_id]);
    }

    const { rows } = await client.query(
      `UPDATE invoices SET date=$1, warehouse_id=$2, payment_status=$3, account_id=$4, branch_id=$5, division_id=$6, vendor_id=$7, reference_number=$8
       WHERE id=$9 RETURNING *`,
      [date, warehouse_id || null, payment_status, account_id || null, branch_id || null, division_id || null, vendor_id || null, reference_number || null, req.params.id]
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

    // Apply new deduction
    const newTotal = items.reduce((s, it) => s + Number(it.quantity) * Number(it.price), 0);
    if (payment_status !== 'unpaid' && account_id) {
      await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [newTotal, account_id]);
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

app.delete('/api/invoices/:id', async (req, res) => {
  const { rows: [inv] } = await pool.query(
    `SELECT inv.invoice_number, inv.payment_status, inv.account_id, inv.photo_path,
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
    await pool.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [inv.total, inv.account_id]);
  }
  await logActivity(pool, { user_id: req.user.id, username: req.user.username, action: 'delete', entity_type: 'invoice', entity_id: req.params.id, description: `Deleted invoice ${inv.invoice_number}` });
  res.status(204).send();
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
      const unit_name = item.units[Number(unit_index)]?.name ?? '';

      const { rows: [src] } = await client.query(
        'SELECT id, quantity, value FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3',
        [item_id, from_warehouse_id, unit_index]
      );
      if (!src || Number(src.quantity) < Number(quantity)) {
        await client.query('ROLLBACK');
        return res.status(400).json({ error: `Insufficient stock for "${item.name}" in ${fw.name} (available: ${src ? Number(src.quantity) : 0} ${unit_name})` });
      }

      const transferredValue = Math.round(Number(src.value) * Number(quantity) / Number(src.quantity));

      await client.query('UPDATE inventory SET quantity=quantity-$1, value=value-$2 WHERE id=$3', [quantity, transferredValue, src.id]);
      await client.query('DELETE FROM inventory WHERE id=$1 AND quantity<=0', [src.id]);

      const { rows: [dst] } = await client.query(
        'SELECT id FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3',
        [item_id, to_warehouse_id, unit_index]
      );
      if (dst) {
        await client.query('UPDATE inventory SET quantity=quantity+$1, value=value+$2 WHERE id=$3', [quantity, transferredValue, dst.id]);
      } else {
        await client.query(
          `INSERT INTO inventory (item_id, warehouse_id, quantity, unit_index, value, date) VALUES ($1,$2,$3,$4,$5,CURRENT_DATE)`,
          [item_id, to_warehouse_id, quantity, unit_index, transferredValue]
        );
      }

      await writeHistory(client, { item_id, warehouse_id: from_warehouse_id, quantity_change: -Number(quantity), unit_name, type: 'manual_out', reference: `Transfer → ${tw.name}`, date: null, source_id: groupId, source_type: 'transfer', value: -transferredValue });
      await writeHistory(client, { item_id, warehouse_id: to_warehouse_id,   quantity_change:  Number(quantity), unit_name, type: 'manual_in',  reference: `Transfer ← ${fw.name}`, date: null, source_id: groupId, source_type: 'transfer', value:  transferredValue });

      const { rows: [transfer] } = await client.query(
        `INSERT INTO stock_transfers (item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name, notes, transferred_by, group_id)
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *`,
        [item_id, from_warehouse_id, to_warehouse_id, quantity, unit_index, unit_name, notes || null, req.user.id, groupId]
      );
      transferRecords.push({ ...transfer, item_name: item.name, unit_name });
    }

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
  const { rows } = await pool.query(
    `SELECT * FROM activity_log ORDER BY created_at DESC LIMIT 200`
  );
  res.json(rows);
});

// ── BRANCHES ──────────────────────────────────────────────────────────────────

app.get('/api/branches', async (req, res) => {
  const { rows } = await pool.query('SELECT * FROM branches ORDER BY name');
  res.json(rows);
});

app.post('/api/branches', async (req, res) => {
  const { name } = req.body;
  if (!name) return res.status(400).json({ error: 'name is required' });
  try {
    const { rows } = await pool.query('INSERT INTO branches (name) VALUES ($1) RETURNING *', [name]);
    res.status(201).json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Branch name already exists' });
    throw e;
  }
});

app.put('/api/branches/:id', async (req, res) => {
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

app.delete('/api/branches/:id', async (req, res) => {
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
    `SELECT d.*, b.name AS branch_name FROM divisions d JOIN branches b ON b.id=d.branch_id ${where} ORDER BY b.name, d.name`,
    params
  );
  res.json(rows);
});

app.post('/api/divisions', async (req, res) => {
  const { branch_id, name } = req.body;
  if (!branch_id || !name) return res.status(400).json({ error: 'branch_id and name are required' });
  try {
    const { rows } = await pool.query(
      'INSERT INTO divisions (branch_id, name) VALUES ($1, $2) RETURNING *',
      [branch_id, name]
    );
    res.status(201).json(rows[0]);
  } catch (e) {
    if (e.code === '23505') return res.status(409).json({ error: 'Division name already exists in this branch' });
    throw e;
  }
});

app.put('/api/divisions/:id', async (req, res) => {
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

app.delete('/api/divisions/:id', async (req, res) => {
  const inUse = await pool.query('SELECT 1 FROM dispatches WHERE division_id=$1 LIMIT 1', [req.params.id]);
  if (inUse.rows.length) return res.status(409).json({ error: 'Division has existing dispatches and cannot be deleted' });
  const { rowCount } = await pool.query('DELETE FROM divisions WHERE id=$1', [req.params.id]);
  if (!rowCount) return res.status(404).json({ error: 'Division not found' });
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
      const unit_name = item.units[Number(unit_index)]?.name ?? '';

      const { rows: [src] } = await client.query(
        'SELECT id, quantity, value FROM inventory WHERE item_id=$1 AND warehouse_id=$2 AND unit_index=$3',
        [item_id, warehouse_id, unit_index]
      );
      if (!src || Number(src.quantity) < Number(quantity)) {
        await client.query('ROLLBACK');
        return res.status(400).json({ error: `Insufficient stock for "${item.name}" in ${wh.name} (available: ${src ? Number(src.quantity) : 0} ${unit_name})` });
      }

      const dispatchedValue = Math.round(Number(src.value) * Number(quantity) / Number(src.quantity));

      await client.query('UPDATE inventory SET quantity=quantity-$1, value=value-$2 WHERE id=$3', [quantity, dispatchedValue, src.id]);
      await client.query('DELETE FROM inventory WHERE id=$1 AND quantity<=0', [src.id]);

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

// ── SALES ─────────────────────────────────────────────────────────────────────

app.get('/api/sales', async (req, res) => {
  const { account_id } = req.query;
  const params = [];
  let where = '';
  if (account_id) { params.push(account_id); where = 'WHERE s.account_id = $1'; }
  const { rows } = await pool.query(
    `SELECT s.*, a.name AS account_name, u.username AS created_by_name
     FROM sales s
     JOIN accounts a ON a.id = s.account_id
     LEFT JOIN users u ON u.id = s.created_by
     ${where}
     ORDER BY s.date DESC, s.created_at DESC`,
    params
  );
  res.json(rows);
});

app.post('/api/sales', async (req, res) => {
  const { account_id, amount, description, date } = req.body;
  if (!account_id || !amount || Number(amount) <= 0) {
    return res.status(400).json({ error: 'Account and a positive amount are required' });
  }
  const client = await pool.connect();
  try {
    await client.query('BEGIN');
    const { rows: [sale] } = await client.query(
      `INSERT INTO sales (account_id, amount, description, date, created_by)
       VALUES ($1, $2, $3, $4, $5) RETURNING *`,
      [account_id, Number(amount), description || null, date || 'today', req.user.id]
    );
    await client.query('UPDATE accounts SET balance = balance + $1 WHERE id = $2', [Number(amount), account_id]);
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
    await client.query('UPDATE accounts SET balance = balance - $1 WHERE id = $2', [sale.amount, sale.account_id]);
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

// ── STATS ─────────────────────────────────────────────────────────────────────

app.get('/api/stats', async (req, res) => {
  const [itemsRes, invRes, valueRes, invoiceRes] = await Promise.all([
    pool.query('SELECT COUNT(*) FROM items'),
    pool.query('SELECT COUNT(*) FROM inventory'),
    pool.query('SELECT COALESCE(SUM(value), 0) AS total FROM inventory'),
    pool.query(`SELECT COUNT(*) FILTER (WHERE payment_status='unpaid') AS unpaid,
                       COUNT(*) AS total FROM invoices`),
  ]);
  res.json({
    totalItems: Number(itemsRes.rows[0].count),
    totalInventoryRecords: Number(invRes.rows[0].count),
    totalValue: Number(valueRes.rows[0].total),
    totalInvoices: Number(invoiceRes.rows[0].total),
    unpaidInvoices: Number(invoiceRes.rows[0].unpaid),
  });
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
