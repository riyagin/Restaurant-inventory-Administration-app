import pg from 'pg';
import bcrypt from 'bcrypt';

const { Pool } = pg;

const [,, username, newPassword] = process.argv;

if (!username || !newPassword) {
  console.error('Usage: node reset-password.js <username> <new-password>');
  process.exit(1);
}

const pool = new Pool({
  host: 'localhost',
  port: 5432,
  database: 'inventory_app',
  user: 'postgres',
  password: 'seesaw',
});

try {
  const { rows } = await pool.query('SELECT id, username FROM users WHERE username = $1', [username]);
  if (!rows.length) {
    console.error(`User "${username}" not found.`);
    process.exit(1);
  }

  const hash = await bcrypt.hash(newPassword, 12);
  await pool.query('UPDATE users SET password_hash = $1 WHERE id = $2', [hash, rows[0].id]);
  console.log(`Password for "${username}" has been reset successfully.`);
} finally {
  await pool.end();
}
