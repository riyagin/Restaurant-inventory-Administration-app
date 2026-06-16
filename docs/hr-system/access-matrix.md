# HR API Access Matrix

Every `/api/hr/*` endpoint × role. Middleware names correspond to `server-go/internal/middleware/auth.go`.

| Endpoint | Method | admin | manager | staff | device-key |
|---|---|:---:|:---:|:---:|:---:|
| **Employees** | | | | | |
| `/api/hr/employees` | GET | Y | Y | Y | — |
| `/api/hr/employees/{id}` | GET | Y | Y | Y | — |
| `/api/hr/employees` | POST | Y | Y | — | — |
| `/api/hr/employees/{id}` | PUT | Y | Y | — | — |
| `/api/hr/employees/{id}` | DELETE | Y | Y | — | — |
| `/api/hr/employees/{id}/photo` | POST | Y | Y | — | — |
| `/api/hr/employees/{id}/photo` | DELETE | Y | Y | — | — |
| **Positions** | | | | | |
| `/api/hr/positions` | GET | Y | Y | Y | — |
| `/api/hr/positions` | POST | Y | Y | — | — |
| `/api/hr/positions/{id}` | PUT | Y | Y | — | — |
| `/api/hr/positions/{id}` | DELETE | Y | Y | — | — |
| **Wage Components** | | | | | |
| `/api/hr/wage-components` | GET | Y | Y | — | — |
| `/api/hr/wage-components` | POST | Y | Y | — | — |
| `/api/hr/wage-components/{id}` | PUT | Y | Y | — | — |
| `/api/hr/wage-components/{id}` | DELETE | Y | Y | — | — |
| **Employee Wage Structures** | | | | | |
| `/api/hr/employees/{id}/wage` | GET | Y | Y | — | — |
| `/api/hr/employees/{id}/wage/history` | GET | Y | Y | — | — |
| `/api/hr/employees/{id}/wage` | POST | Y | Y | — | — |
| **Bulk Import** | | | | | |
| `/api/hr/import/template` | GET | Y | Y | — | — |
| `/api/hr/import/parse` | POST | Y | Y | — | — |
| `/api/hr/import/confirm` | POST | Y | Y | — | — |
| **Attendance (JWT)** | | | | | |
| `/api/hr/attendance` | GET | Y | Y | — | — |
| `/api/hr/attendance/{id}` | PUT | Y | Y | — | — |
| `/api/hr/attendance/reconcile` | POST | Y | Y | — | — |
| `/api/hr/attendance/fingerprint-import/parse` | POST | Y | Y | — | — |
| `/api/hr/attendance/fingerprint-import/confirm` | POST | Y | Y | — | — |
| `/api/hr/attendance/work-schedules` | GET | Y | Y | — | — |
| `/api/hr/attendance/work-schedules` | POST | Y | Y | — | — |
| `/api/hr/attendance/holidays` | GET | Y | Y | — | — |
| `/api/hr/attendance/holidays` | POST | Y | Y | — | — |
| `/api/hr/attendance/holidays/{id}` | DELETE | Y | Y | — | — |
| `/api/hr/attendance/devices` | GET | Y | Y | — | — |
| `/api/hr/attendance/devices` | POST | Y | Y | — | — |
| `/api/hr/attendance/devices/{id}` | PUT | Y | Y | — | — |
| `/api/hr/attendance/devices/{id}` | DELETE | Y | Y | — | — |
| **Attendance (Device key — no JWT)** | | | | | |
| `/api/hr/attendance/device/event` | POST | — | — | — | Y |
| `/api/hr/attendance/device/employees` | GET | — | — | — | Y |
| **Performance** | | | | | |
| `/api/hr/performance/policies` | GET | Y | Y | — | — |
| `/api/hr/performance/policies` | POST | Y | Y | — | — |
| `/api/hr/performance/policies/{id}` | PUT | Y | Y | — | — |
| `/api/hr/performance/policies/{id}` | DELETE | Y | Y | — | — |
| `/api/hr/performance/scores` | GET | Y | Y | — | — |
| `/api/hr/employees/{id}/performance` | GET | Y | Y | — | — |
| `/api/hr/performance/violations` | POST | Y | Y | — | — |
| `/api/hr/performance/violations/{id}` | DELETE | Y | Y | — | — |
| `/api/hr/performance/evaluate` | POST | Y | Y | — | — |
| **Leave** | | | | | |
| `/api/hr/leave-types` | GET | Y | Y | — | — |
| `/api/hr/leave-types` | POST | Y | Y | — | — |
| `/api/hr/leave-types/{id}` | PUT | Y | Y | — | — |
| `/api/hr/leave-types/{id}` | DELETE | Y | Y | — | — |
| `/api/hr/leave-requests` | GET | Y | Y | — | — |
| `/api/hr/leave-requests` | POST | Y | Y | — | — |
| `/api/hr/leave-requests/{id}/cancel` | POST | Y | Y | — | — |
| `/api/hr/employees/{id}/leave-balance` | GET | Y | Y | — | — |
| `/api/hr/employees/{id}/leave-balance` | PUT | Y | Y | — | — |
| `/api/hr/employees/{id}/leave-requests` | GET | Y | Y | — | — |
| `/api/hr/leave-requests/{id}/approve` | POST | — | Y | — | — |
| `/api/hr/leave-requests/{id}/reject` | POST | — | Y | — | — |
| **Kasbon** | | | | | |
| `/api/hr/kasbons` | GET | Y | Y | — | — |
| `/api/hr/kasbons` | POST | Y | Y | — | — |
| `/api/hr/kasbons/{id}` | GET | Y | Y | — | — |
| `/api/hr/kasbons/{id}` | PUT | Y | Y | — | — |
| `/api/hr/kasbons/{id}/process` | POST | Y | Y | — | — |
| `/api/hr/kasbons/{id}/cancel` | POST | Y | Y | — | — |
| `/api/hr/kasbons/{id}/approve` | POST | — | Y | — | — |
| `/api/hr/kasbons/{id}/reject` | POST | — | Y | — | — |
| **Payroll** | | | | | |
| `/api/hr/payroll/periods` | GET | Y | Y | — | — |
| `/api/hr/payroll/periods` | POST | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}` | GET | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}/lines` | GET | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}/regenerate-line/{employeeId}` | POST | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}/close` | POST | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}/mark-paid` | POST | Y | Y | — | — |
| `/api/hr/payroll/lines/{id}/review` | GET | Y | Y | — | — |
| `/api/hr/payroll/lines/{id}/review` | POST | Y | Y | — | — |
| `/api/hr/payroll/lines/{id}/unreview` | POST | Y | Y | — | — |
| `/api/hr/payroll/lines/{id}/payslip` | GET | Y | Y | — | — |
| `/api/hr/payroll/periods/{id}/payslips` | GET | Y | Y | — | — |
| **HR Settings** | | | | | |
| `/api/hr/settings` | GET | Y | Y | — | — |
| `/api/hr/settings` | PUT | Y | — | — | — |
| `/api/hr/settings/logo` | POST | Y | — | — | — |

## Notes

- **admin** and **manager** share `RequireAdminOrManager` middleware for most HR routes. `RequireAdmin` (used on non-HR routes) also accepts manager for backward-compatibility (see `auth.go`).
- **manager-only** endpoints (approve/reject for kasbon and leave) use `RequireManager` which only accepts `role = manager`.
- **staff** has read access to `/api/hr/employees` and `/api/hr/positions` (no middleware guard on those two GETs); all other HR endpoints return 403 for staff.
- **device-key** endpoints sit outside the JWT middleware group and authenticate via `X-Device-Key` header (`DeviceAuth` middleware). JWT tokens are rejected on these routes.
