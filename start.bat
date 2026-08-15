@echo off
title Inventory App Launcher

:: Detect LAN IPv4 (skip loopback 127.x and APIPA 169.x)
for /f %%i in ('powershell -NoProfile -Command ^
  "(Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.*' } | Sort-Object PrefixLength | Select-Object -First 1).IPAddress"') do set LAN_IP=%%i

if "%LAN_IP%"=="" (
  echo WARNING: Could not detect LAN IP, falling back to localhost
  set LAN_IP=localhost
)

echo Detected LAN IP: %LAN_IP%

:: Write config.json so the frontend points to this machine's IP
echo { "apiBaseUrl": "http://%LAN_IP%:5000/api" } > "%~dp0client\public\config.json"

echo Starting Go backend server...
start "Backend" cmd /k "cd /d "%~dp0server-go" && go run ./cmd/api"

echo Starting frontend (LAN-accessible)...
start "Frontend" cmd /k "cd /d "%~dp0client" && npm run dev -- --host"

echo Waiting for servers to start...
timeout /t 4 /nobreak >nul

echo Opening browser...
start http://localhost:5173

echo.
echo =============================================
echo   App running on this machine:
echo     http://localhost:5173
echo.
echo   App accessible from LAN devices:
echo     http://%LAN_IP%:5173
echo.
echo   API:  http://%LAN_IP%:5000/api
echo =============================================
echo Close the Backend and Frontend windows to stop.
