#!/bin/bash
set -euo pipefail

BASE="http://localhost:5000"
PASS=0
FAIL=0

check() {
  local label="$1"
  local status="$2"
  if [ "$status" -ge 200 ] && [ "$status" -lt 400 ]; then
    echo "  OK   $label ($status)"
    PASS=$((PASS + 1))
  else
    echo "  FAIL $label ($status)"
    FAIL=$((FAIL + 1))
  fi
}

echo "=== Auth ==="
LOGIN=$(curl -s -w "\n%{http_code}" -X POST "$BASE/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}')
LOGIN_BODY=$(echo "$LOGIN" | head -n1)
LOGIN_STATUS=$(echo "$LOGIN" | tail -n1)
check "POST /api/auth/login" "$LOGIN_STATUS"
TOKEN=$(echo "$LOGIN_BODY" | jq -r '.token // empty')
if [ -z "$TOKEN" ]; then
  echo "Could not obtain token — aborting."
  exit 1
fi
echo "  Token: ${TOKEN:0:30}..."

echo ""
echo "=== Health ==="
check "GET /api/health" "$(curl -s -o /dev/null -w "%{http_code}" "$BASE/api/health")"

echo ""
echo "=== Core data ==="
AUTH="-H \"Authorization: Bearer $TOKEN\""
check "GET /api/items"      "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/items")"
check "GET /api/inventory"  "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/inventory")"
check "GET /api/warehouses" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/warehouses")"
check "GET /api/vendors"    "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/vendors")"
check "GET /api/accounts"   "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/accounts")"
check "GET /api/branches"   "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/branches")"
check "GET /api/divisions"  "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/divisions")"

echo ""
echo "=== Invoices & transfers ==="
check "GET /api/invoices"           "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/invoices")"
check "GET /api/stock-transfers"    "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/stock-transfers")"
check "GET /api/stock-opname"       "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/stock-opname")"
check "GET /api/dispatches"         "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/dispatches")"

echo ""
echo "=== Production & sales ==="
check "GET /api/recipes"     "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/recipes")"
check "GET /api/productions" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/productions")"
check "GET /api/sales"       "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/sales")"
check "GET /api/pos-import"  "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/pos-import")"
check "GET /api/enumerations" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/enumerations")"

echo ""
echo "=== Stats ==="
check "GET /api/stats"             "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/stats")"
check "GET /api/stats/daily-sales" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/stats/daily-sales")"
check "GET /api/stats/stock-flow"  "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/stats/stock-flow")"

echo ""
echo "=== Reports ==="
FROM=$(date -d "30 days ago" +%Y-%m-%d 2>/dev/null || date -v-30d +%Y-%m-%d)
TO=$(date +%Y-%m-%d)
check "GET /api/reports/financial"       "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/reports/financial?from=$FROM&to=$TO")"
check "GET /api/reports/daily"           "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/reports/daily?from=$FROM&to=$TO")"
check "GET /api/reports/inventory-value" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/reports/inventory-value")"
check "GET /api/reports/expense-summary" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/reports/expense-summary?from=$FROM&to=$TO")"
check "GET /api/expense-report"          "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/expense-report?from=$FROM&to=$TO")"

echo ""
echo "=== Admin — activity log & adjustments ==="
check "GET /api/activity-log"        "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/activity-log")"
check "GET /api/account-adjustments" "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/account-adjustments")"
check "GET /api/users"               "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/users")"
check "GET /api/invoice-templates"   "$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" "$BASE/api/invoice-templates")"

echo ""
echo "=== Summary ==="
echo "  Passed: $PASS"
echo "  Failed: $FAIL"
[ "$FAIL" -eq 0 ] && echo "  All checks passed." || echo "  Some checks failed — review output above."
exit "$FAIL"
