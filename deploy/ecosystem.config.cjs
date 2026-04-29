module.exports = {
  apps: [
    {
      name: 'inventory-app',
      script: './server/index.js',
      cwd: '/var/www/inventory-app',

      // Restart automatically on crash
      autorestart: true,
      watch: false,
      max_restarts: 10,
      restart_delay: 3000,

      // Environment variables for production
      env_production: {
        NODE_ENV: 'production',
        PORT: 5000,
        JWT_SECRET: 'CHANGE_THIS_TO_A_LONG_RANDOM_STRING',
        DB_HOST: 'localhost',
        DB_PORT: 5432,
        DB_NAME: 'inventory_app',
        DB_USER: 'postgres',
        DB_PASSWORD: 'CHANGE_THIS_TO_YOUR_DB_PASSWORD',
      },

      // Log files
      out_file: '/var/log/inventory-app/out.log',
      error_file: '/var/log/inventory-app/error.log',
      log_date_format: 'YYYY-MM-DD HH:mm:ss',
      merge_logs: true,

      // Rotate logs when they reach 10MB, keep last 7 days
      max_size: '10M',
      retain: 7,
    },
  ],
};
