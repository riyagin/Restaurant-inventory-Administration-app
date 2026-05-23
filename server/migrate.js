#!/usr/bin/env node
// Runs all SQL files in server/migrations/ that haven't been applied yet.
// Tracks applied migrations in a `schema_migrations` table.

import pg from 'pg';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import { dbConfig } from './config.js';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const MIGRATIONS_DIR = path.join(__dirname, 'migrations');

const pool = new pg.Pool(dbConfig);

async function run() {
  const client = await pool.connect();
  try {
    await client.query(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        filename   TEXT PRIMARY KEY,
        applied_at TIMESTAMPTZ DEFAULT NOW()
      )
    `);

    const { rows: applied } = await client.query('SELECT filename FROM schema_migrations');
    const appliedSet = new Set(applied.map(r => r.filename));

    const files = fs.readdirSync(MIGRATIONS_DIR)
      .filter(f => f.endsWith('.sql'))
      .sort();

    const pending = files.filter(f => !appliedSet.has(f));

    if (pending.length === 0) {
      console.log('No pending migrations.');
      return;
    }

    for (const file of pending) {
      const sql = fs.readFileSync(path.join(MIGRATIONS_DIR, file), 'utf8');
      console.log(`Applying: ${file}`);
      await client.query('BEGIN');
      try {
        await client.query(sql);
        await client.query('INSERT INTO schema_migrations (filename) VALUES ($1)', [file]);
        await client.query('COMMIT');
        console.log(`  ✓ Done`);
      } catch (err) {
        await client.query('ROLLBACK');
        console.error(`  ✗ Failed: ${err.message}`);
        process.exit(1);
      }
    }

    console.log(`\n${pending.length} migration(s) applied.`);
  } finally {
    client.release();
    await pool.end();
  }
}

run().catch(err => {
  console.error(err);
  process.exit(1);
});
