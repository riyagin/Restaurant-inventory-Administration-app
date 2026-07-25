ALTER TABLE employees
  DROP COLUMN IF EXISTS face_embedding,
  DROP COLUMN IF EXISTS face_embedding_version,
  DROP COLUMN IF EXISTS face_enrolled_at;
