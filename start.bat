@echo off
title Inventory App Launcher

echo Starting backend server...
start "Backend" cmd /k "cd /d "%~dp0server" && npm run dev"

echo Starting frontend...
start "Frontend" cmd /k "cd /d "%~dp0client" && npm run dev"

echo Waiting for servers to start...
timeout /t 4 /nobreak >nul

echo Opening browser...
start http://localhost:5173

echo.
echo Both servers are running.
echo Close the Backend and Frontend windows to stop the app.
