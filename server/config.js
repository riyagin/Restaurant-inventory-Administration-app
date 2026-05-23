import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import path from 'path';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const configPath = path.join(__dirname, 'config.json');

let fileConfig = {};
try {
  fileConfig = JSON.parse(readFileSync(configPath, 'utf8'));
} catch {
  // config.json is optional — fall back to env vars or defaults
}

const db = fileConfig.db ?? {};

export const dbConfig = {
  host:     process.env.DB_HOST     || db.host     || 'localhost',
  port:     Number(process.env.DB_PORT || db.port  || 5432),
  database: process.env.DB_NAME     || db.database || 'inventory_app',
  user:     process.env.DB_USER     || db.user     || 'postgres',
  password: process.env.DB_PASSWORD || db.password || '',
};

export const jwtSecret =
  process.env.JWT_SECRET || fileConfig.jwtSecret || 'inventory_secret_change_in_prod';
