@echo off
REM ============================================================
REM  HR System verification — runs migrations + tests + lint.
REM  Run from the repo root:  verify-hr.bat
REM  Requires: Go, sqlc, golang-migrate (migrate), Node/npm,
REM  and a running PostgreSQL with the inventory_app database.
REM ============================================================
setlocal

REM --- Build DATABASE_URL from server-go\.env defaults ---
REM Adjust if your credentials differ.
set "DATABASE_URL=postgres://postgres:seesaw@localhost:5432/inventory_app?sslmode=disable"

echo.
echo ===== [1/7] go mod tidy (pulls go-pdf/fpdf for payslips) =====
pushd server-go
go mod tidy || goto :fail

echo.
echo ===== [2/7] sqlc generate (regenerate internal/db) =====
sqlc generate || goto :fail
echo  - If this changed files, run: git diff internal\db   (hand-written vs generated)

echo.
echo ===== [3/7] go build ./... =====
go build ./... || goto :fail

echo.
echo ===== [4/7] go test ./... =====
go test ./... || goto :fail

echo.
echo ===== [5/7] migrate UP =====
migrate -path migrations -database "%DATABASE_URL%" up || goto :fail

echo.
echo ===== [6/7] migrate DOWN 1 then UP (verify newest migration reverses) =====
migrate -path migrations -database "%DATABASE_URL%" down 1 || goto :fail
migrate -path migrations -database "%DATABASE_URL%" up || goto :fail
popd

echo.
echo ===== [7/7] client lint =====
pushd client
call npm run lint || goto :fail
popd

echo.
echo ============================================================
echo  ALL CHECKS PASSED
echo ============================================================
endlocal
exit /b 0

:fail
echo.
echo ************************************************************
echo  FAILED at the step above. Copy the error output and send
echo  it back so the issue can be fixed.
echo ************************************************************
popd 2>nul
endlocal
exit /b 1
