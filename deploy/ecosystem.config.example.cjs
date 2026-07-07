// TEMPLATE — copy to `ecosystem.config.cjs` and fill in real values.
//
//   cp ecosystem.config.example.cjs ecosystem.config.cjs
//   # then edit DB_PASSWORD and JWT_SECRET below
//
// The real `ecosystem.config.cjs` is gitignored so it can hold production
// secrets without being committed or overwritten by `git pull` on deploy.
//
// NOTE: PM2 caches the env block in ~/.pm2/dump.pm2. After editing values,
// reload the env explicitly — a plain `pm2 restart` keeps the cached values:
//   pm2 restart inventory-app --update-env
//   # or: pm2 delete inventory-app && pm2 start ecosystem.config.cjs
module.exports = {
  apps: [
    {
      name: 'inventory-app',
      script: './api',
      cwd: '/var/www/inventory-app/server-go',
      interpreter: 'none', // run the Go binary directly so PM2 tracks the real PID
                           // (prevents orphaned instances that keep holding :5000)

      autorestart: true,
      watch: false,
      max_restarts: 10,
      restart_delay: 3000,
      max_memory_restart: '200M',
      kill_timeout: 6000, // allow the 5s graceful shutdown to finish before SIGKILL

      env: {
        PORT: 5000,
        DB_HOST: 'localhost',
        DB_PORT: 5432,
        DB_NAME: 'inventory_app',
        DB_USER: 'postgres',
        DB_PASSWORD: 'CHANGE_THIS_TO_YOUR_DB_PASSWORD',
        JWT_SECRET: 'CHANGE_THIS_TO_A_LONG_RANDOM_STRING',
        UPLOADS_DIR: '/var/www/inventory-app/server/uploads',
      },

      out_file: '/var/log/inventory-app/out.log',
      error_file: '/var/log/inventory-app/error.log',
      log_date_format: 'YYYY-MM-DD HH:mm:ss',
      merge_logs: true,
    },
  ],
};
