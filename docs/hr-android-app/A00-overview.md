# Android Attendance App — Master Plan & Shared Context

> **Read this file first in every session.** Prompts A01–A05 build a **separate project** (its own repo/folder, NOT inside inventory-app): a native Android app for employee attendance via on-device facial recognition. Run prompts in order.

## Run Order

| # | Prompt | Depends on |
|---|---|---|
| A01 | `A01-project-setup.md` — Project scaffold, device registration, API client | backend prompt 04 deployed |
| A02 | `A02-face-enrollment.md` — Employee roster sync + face enrollment | A01 |
| A03 | `A03-checkin-flow.md` — Live detection, matching, check-in/out submission | A02 |
| A04 | `A04-offline-sync.md` — Offline queue, retry, conflict handling | A03 |
| A05 | `A05-kiosk-and-qa.md` — Kiosk mode, settings, hardening, QA | A04 |

## Product Summary

A tablet/phone mounted at each branch runs this app in kiosk style. An employee walks up, the app detects their face, matches it against enrolled employees of that branch **on-device**, and submits a check-in/check-out event (with the captured photo) to the inventory-app Go backend. Face is the **primary** attendance source (fingerprint import on the backend is the backup).

## Tech Stack

| Concern | Choice |
|---|---|
| Language / UI | Kotlin, Jetpack Compose, Material 3 |
| Min SDK | 26 (Android 8.0) |
| Camera | CameraX |
| Face detection | Google ML Kit Face Detection (on-device) |
| Face matching | On-device embeddings: TFLite FaceNet/MobileFaceNet model; cosine similarity against enrolled embeddings |
| Local DB | Room (employees, embeddings, pending events queue) |
| Networking | Retrofit + OkHttp + kotlinx.serialization |
| Background sync | WorkManager |
| DI | Hilt |

## Backend API Contract (defined by `docs/hr-system/04-attendance.md`)

Base URL configurable in app settings. Auth: header `X-Device-Key: <key>` issued when the admin registers the device in the web app (`attendance_devices`).

- `GET /api/hr/attendance/device/employees` → branch roster: `[{ employee_code, full_name, photo_path }]`
- `POST /api/hr/attendance/device/event` — multipart: `employee_code`, `event_type` (`auto` recommended — server decides in/out), `timestamp` (RFC3339, device time), `photo` (JPEG of the matched frame). Response includes resolved state (`check_in`/`check_out`, time) for the greeting screen.

If a needed field is missing from the contract during implementation, **do not invent server changes** — note it in `API-GAPS.md` at the repo root for the backend team.

## Conventions

1. All UI text **Indonesian** (mirrors the web app).
2. Face embeddings never leave the device except as implied by the enrollment photo upload decision in A02; the check-in **photo** is sent as evidence (this is expected behavior).
3. Match threshold and liveness settings configurable in the admin settings screen (A05), with safe defaults.
4. Every prompt ends with: project compiles (`./gradlew assembleDebug`), unit tests pass (`./gradlew test`), no lint errors (`./gradlew lint`).
5. Repo name suggestion: `attendance-app/`. Kotlin official code style, package `com.<company>.attendance`.
