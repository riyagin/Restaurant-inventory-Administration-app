# Prompt A01 — Project Scaffold, Device Registration & API Client

> Read `docs/hr-android-app/A00-overview.md` first. This creates a NEW standalone Android project.

## Goal

Bootstrap the app: Gradle project, architecture skeleton, settings storage, device-key onboarding flow, and a working authenticated API client against the Go backend.

## Tasks

1. **Project scaffold**: Kotlin + Jetpack Compose + Material 3, single-activity, Hilt DI, version catalogs (`libs.versions.toml`). Modules: single `app` module is fine. Packages: `ui/`, `data/` (api, db, repo), `domain/`, `di/`.
2. **Settings (DataStore)**: `server_base_url`, `device_key`, `device_name`, `match_threshold` (default 0.75), `kiosk_pin` (set later). Settings repository + simple debug screen to view values.
3. **Onboarding flow** (first launch, or when no device key):
   - Screen 1: input server URL → "Tes Koneksi" pings `GET <base>/api/hr/attendance/device/employees` expecting 401 (proves reachability).
   - Screen 2: input device key (manual paste; also support QR scan via ML Kit barcode — the web app shows the key once at device registration). Validate by calling the roster endpoint expecting 200.
   - Persist and proceed to a placeholder Home screen showing connection status + branch roster count.
4. **API client**: Retrofit service `AttendanceApi` with the two contract endpoints (A00). OkHttp interceptor adds `X-Device-Key`. Timeouts ~15s; kotlinx.serialization models. Error envelope → sealed `ApiResult` type.
5. **Room DB** scaffold: entities `EmployeeEntity` (code, name, photoPath/photoUrl, embedding BLOB nullable, enrolledAt) and `PendingEventEntity` (filled in A04) + DAOs. Migrations strategy: `fallbackToDestructiveMigration` is acceptable pre-1.0.
6. **CI sanity**: Gradle wrapper committed; `./gradlew assembleDebug test lint` green; README with build/run instructions and the API contract summary.

## UI Notes

Indonesian text. Keep onboarding minimal and tablet-friendly (the device will usually be a mounted tablet). Landscape and portrait both supported.

## Definition of Done

Fresh clone builds; onboarding completes against a live/dev backend (or MockWebServer test proving the flow); device key persisted; roster fetch unit-tested with MockWebServer.
