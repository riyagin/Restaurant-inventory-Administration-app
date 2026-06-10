# Prompt A02 — Roster Sync & Face Enrollment

> Read `docs/hr-android-app/A00-overview.md` first. Requires A01 completed.

## Goal

Sync the branch employee roster from the backend and enroll each employee's face: capture reference images on-device, compute embeddings with a TFLite model, store locally for matching in A03.

## Tasks

1. **Roster sync**: `SyncRepository.syncEmployees()` — fetch `GET /device/employees`, upsert into Room (match by `employee_code`; remove employees no longer in the roster, keeping their enrollment data tombstoned for 30 days in case of accidental roster changes). Manual "Sinkronkan" button + automatic daily WorkManager sync.
2. **Embedding pipeline** (`domain/face/`):
   - ML Kit Face Detection to find/crop/align the face (use landmark-based alignment, 112×112 input).
   - TFLite **MobileFaceNet** (or FaceNet-512) model bundled in `assets/` producing a float embedding; L2-normalize. Document the model source/license in README.
   - `FaceEmbedder.embed(bitmap): FloatArray` with unit tests using fixture images.
3. **Enrollment screen** (admin-gated later by kiosk PIN, A05):
   - Employee list with enrollment status badge (Terdaftar / Belum Terdaftar).
   - Tap employee → guided capture: CameraX preview, ML Kit live detection overlay, capture **3–5 samples** (prompt slight angle changes: "Hadap lurus", "Sedikit ke kiri", ...). Quality gates per sample: exactly one face, min face size, eyes open probability, not blurry (Laplacian variance check).
   - Store the **average of sample embeddings** (plus each sample embedding for threshold tuning) in Room. Save one reference JPEG locally for the admin to verify identity visually.
   - Re-enroll flow replaces previous data.
4. **Enrollment quality check**: after capture, verify samples agree with each other (pairwise cosine similarity above threshold); otherwise ask to redo.

## Non-Goals

No backend upload of embeddings (matching is fully on-device). If the backend roster lacks something needed, log it in `API-GAPS.md`.

## Definition of Done

Build/test/lint green; enrollment produces stable embeddings (test: same fixture face → similarity > threshold; different faces → below); roster sync idempotent.
