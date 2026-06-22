@echo off
setlocal EnableDelayedExpansion
title Reset Attendance Data

:: ---------------------------------------------------------------
:: Usage:
::   reset-attendance.bat          -- resets today's records
::   reset-attendance.bat 2026-06-22  -- resets a specific date
::   reset-attendance.bat all       -- resets ALL attendance data
:: ---------------------------------------------------------------

:: Parse date argument
set "TARGET=%~1"
if "%TARGET%"=="" (
  for /f %%d in ('powershell -NoProfile -Command "Get-Date -Format yyyy-MM-dd"') do set TARGET=%%d
)

:: Load DB credentials from server-go/.env
set "ENV_FILE=%~dp0server-go\.env"
if not exist "%ENV_FILE%" (
  echo ERROR: %ENV_FILE% not found
  exit /b 1
)

for /f "usebackq tokens=1,* delims==" %%a in ("%ENV_FILE%") do (
  if "%%a"=="DB_HOST"     set DB_HOST=%%b
  if "%%a"=="DB_PORT"     set DB_PORT=%%b
  if "%%a"=="DB_NAME"     set DB_NAME=%%b
  if "%%a"=="DB_USER"     set DB_USER=%%b
  if "%%a"=="DB_PASSWORD" set DB_PASSWORD=%%b
)

if "%DB_HOST%"==""  set DB_HOST=localhost
if "%DB_PORT%"==""  set DB_PORT=5432
if "%DB_NAME%"==""  set DB_NAME=inventory_app
if "%DB_USER%"==""  set DB_USER=postgres

:: Build the SQL depending on mode
if /i "%TARGET%"=="all" (
  echo.
  echo WARNING: This will delete ALL attendance records, fingerprint imports,
  echo          auto-generated performance violations, and performance scores.
  echo.
  set /p CONFIRM="Type YES to continue: "
  if /i not "!CONFIRM!"=="YES" (
    echo Aborted.
    exit /b 0
  )
  set "SQL=DELETE FROM performance_violations WHERE source = 'auto'; DELETE FROM performance_scores; DELETE FROM fingerprint_imports; DELETE FROM attendance_records;"
  set "DESC=ALL attendance data"
) else (
  echo.
  echo Resetting attendance data for date: %TARGET%
  echo.
  set "SQL=DELETE FROM performance_violations WHERE source = 'auto' AND attendance_record_id IN (SELECT id FROM attendance_records WHERE date = '%TARGET%'); DELETE FROM performance_scores WHERE period_month = date_trunc('month', '%TARGET%'::date); DELETE FROM fingerprint_imports WHERE created_at::date = '%TARGET%'::date; DELETE FROM attendance_records WHERE date = '%TARGET%';"
  set "DESC=attendance data for %TARGET%"
)

echo Running SQL against %DB_NAME% on %DB_HOST%:%DB_PORT%...
echo.

set PGPASSWORD=%DB_PASSWORD%
psql -h %DB_HOST% -p %DB_PORT% -U %DB_USER% -d %DB_NAME% -c "!SQL!"

if %errorlevel% neq 0 (
  echo.
  echo ERROR: psql failed. Make sure psql is in your PATH and the DB is running.
  exit /b 1
)

echo.
echo Done. Cleared %DESC%.
endlocal
