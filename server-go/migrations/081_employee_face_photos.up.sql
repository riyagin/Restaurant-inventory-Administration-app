-- Face photos sent by the attendance devices, so HR can see what each employee
-- currently looks like. A photo arrives in two ways:
--
--   source = 'enroll'  : the reference shot taken when the face was registered on
--                        a kiosk (uploaded with the embedding).
--   source = 'checkin' : the face crop from the first face check-in of a day. At
--                        most one per employee per day, so the profile photo is
--                        refreshed daily without storing every scan.
--
-- Storage is bounded by a background purge (service.PurgeOldFacePhotos): photos
-- older than two days are deleted, except each employee's latest photo, which is
-- always kept. So a resigned employee (who stops checking in) keeps their last
-- photo until HR deletes it explicitly.
CREATE TABLE IF NOT EXISTS employee_face_photos (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  employee_id UUID        NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
  photo_path  TEXT        NOT NULL,
  source      TEXT        NOT NULL CHECK (source IN ('enroll', 'checkin')),
  captured_at TIMESTAMPTZ NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves both "latest photo for this employee" and the purge's per-employee scan.
CREATE INDEX IF NOT EXISTS idx_employee_face_photos_employee_captured
  ON employee_face_photos (employee_id, captured_at DESC);
