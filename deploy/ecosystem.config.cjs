module.exports = {
  apps: [
    {
      name: 'inventory-app',
      script: './api',
      cwd: '/var/www/inventory-app/server-go',

      autorestart: true,
      watch: false,
      max_restarts: 10,
      restart_delay: 3000,
      max_memory_restart: '200M',

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

      max_size: '10M',
      retain: 7,
    },
  ],
};
