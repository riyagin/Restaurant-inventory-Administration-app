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
                           // (prevents orphaned instances that keep holding the port)

      autorestart: true,
      watch: false,
      max_restarts: 10,
      restart_delay: 3000,
      // min_uptime is what makes max_restarts mean anything. PM2's default is
      // 1000ms, so a process that stays up a few seconds before dying resets the
      // restart counter every time — the cap is never reached and the app
      // restarts forever instead of settling into `errored` where it is visible.
      min_uptime: '30s',
      // NOT 200M. This is a Go binary that parses whole Excel workbooks into
      // memory (POS import, HR import, fingerprint import, policy import) and
      // renders a period's payslips into a PDF/ZIP. Any of those blows past
      // 200MB, and Go's GC does not hand freed pages straight back to the OS, so
      // RSS stays high afterwards — PM2 kills it, it comes back, someone runs
      // another import, it is killed again. That loop has no error in the app
      // log, because the app never failed. Halve both figures on a 1GB VPS.
      max_memory_restart: '1G',
      kill_timeout: 6000, // allow the 5s graceful shutdown to finish before SIGKILL

      env: {
        // 5002, not 5000: an orphaned instance of a previous deploy held :5000
        // and could not be reclaimed. 5001 is the legacy Node backend's port.
        // Must match proxy_pass in deploy/nginx.conf.
        PORT: 5002,
        // Keep Go's own ceiling below max_memory_restart so the runtime GCs
        // harder as it approaches the limit instead of being killed at it.
        GOMEMLIMIT: '768MiB',
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
