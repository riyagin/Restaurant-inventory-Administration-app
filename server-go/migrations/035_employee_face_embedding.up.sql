-- Server-side face enrollment. Previously each attendance device stored its own
-- face embeddings locally; there was no shared source of truth, so a face enrolled
-- on one kiosk was invisible to every other device. We now store the enrollment on
-- the server so any device can sync it down and match locally.
--
-- face_embedding         : the L2-normalized average embedding produced on-device
--                          (a packed little-endian float32 vector, stored as bytes).
-- face_embedding_version : the model + preprocessing space the embedding lives in
--                          (e.g. "edgeface-base-512-v1"). Embeddings are only valid
--                          within one space; a device must ignore a server embedding
--                          whose version does not match its active model, otherwise it
--                          would match randomly. Enrolling with a new model overwrites
--                          this per employee.
-- face_enrolled_at       : when the stored embedding was captured (audit / display).
ALTER TABLE employees
  ADD COLUMN IF NOT EXISTS face_embedding         BYTEA,
  ADD COLUMN IF NOT EXISTS face_embedding_version TEXT,
  ADD COLUMN IF NOT EXISTS face_enrolled_at       TIMESTAMPTZ;
