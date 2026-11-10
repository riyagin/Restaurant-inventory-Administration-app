package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Face photos sent by attendance devices (table employee_face_photos). See
// migration 081 for the model. Files live in the shared uploads directory and
// are served at /uploads/<photo_path>, same as every other upload.
//
// Retention, per employee:
//   - the newest photo is always kept, so anyone who has ever had a photo
//     keeps at least one (a resigned employee keeps their last photo until HR
//     deletes it);
//   - other photos are kept for FacePhotoRetention;
//   - never more than FacePhotoMaxPerEmployee photos.
//
// Attendance records do not keep photos. A check-in photo only becomes a face
// photo when it is the employee's first face check-in of the day.

// FacePhotoRetention is how long a photo other than the newest is kept.
const FacePhotoRetention = 48 * time.Hour

// FacePhotoMaxPerEmployee caps how many face photos one employee can have.
const FacePhotoMaxPerEmployee = 6

const (
	FacePhotoSourceEnroll  = "enroll"
	FacePhotoSourceCheckin = "checkin"
)

// SaveFacePhoto writes the image to the uploads directory, records it as a face
// photo for the employee, then trims that employee's photos to the retention
// rules. It returns the stored file name.
func SaveFacePhoto(ctx context.Context, pool *pgxpool.Pool, uploadsDir string,
	employeeID pgtype.UUID, src io.Reader, source string, capturedAt time.Time) (string, error) {

	fname, err := writeFacePhotoFile(uploadsDir, employeeID, src)
	if err != nil {
		return "", err
	}
	if _, err := pool.Exec(ctx, insertFacePhotoSQL, employeeID, fname, source, capturedAt); err != nil {
		os.Remove(filepath.Join(uploadsDir, fname))
		return "", fmt.Errorf("insert face photo: %w", err)
	}
	trimEmployeeFacePhotos(ctx, pool, uploadsDir, employeeID)
	return fname, nil
}

// RecordDailyCheckinPhoto stores a check-in photo as the employee's face photo,
// but only for the first face check-in of a day. The day comes from the event's
// own timestamp and offset (the same rule the attendance record uses). The photo
// is skipped when the employee already has a photo from that day or later, so a
// late-syncing offline event never adds a photo older than the newest one.
// Returns the new file name, or "" when nothing was stored.
func RecordDailyCheckinPhoto(ctx context.Context, pool *pgxpool.Pool, uploadsDir string,
	employeeID pgtype.UUID, photo []byte, eventTime time.Time) (string, error) {

	dayStart := time.Date(eventTime.Year(), eventTime.Month(), eventTime.Day(), 0, 0, 0, 0, eventTime.Location())

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Two kiosks can send the same employee's first check-in at the same moment.
	// Serialise per employee so only one of them stores the day's photo.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 81))`, employeeID); err != nil {
		return "", fmt.Errorf("lock: %w", err)
	}

	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1 FROM employee_face_photos
		  WHERE employee_id = $1 AND captured_at >= $2
		)`, employeeID, dayStart).Scan(&exists); err != nil {
		return "", fmt.Errorf("check daily photo: %w", err)
	}
	if exists {
		return "", nil
	}

	fname, err := writeFacePhotoFile(uploadsDir, employeeID, bytes.NewReader(photo))
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, insertFacePhotoSQL, employeeID, fname, FacePhotoSourceCheckin, eventTime); err != nil {
		os.Remove(filepath.Join(uploadsDir, fname))
		return "", fmt.Errorf("insert face photo: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		os.Remove(filepath.Join(uploadsDir, fname))
		return "", err
	}
	trimEmployeeFacePhotos(ctx, pool, uploadsDir, employeeID)
	return fname, nil
}

// LatestFacePhoto returns the employee's most recent face photo, if any.
func LatestFacePhoto(ctx context.Context, pool *pgxpool.Pool, employeeID pgtype.UUID) (path string, capturedAt time.Time, ok bool, err error) {
	err = pool.QueryRow(ctx, `
		SELECT photo_path, captured_at
		FROM employee_face_photos
		WHERE employee_id = $1
		ORDER BY captured_at DESC, created_at DESC
		LIMIT 1`, employeeID).Scan(&path, &capturedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", time.Time{}, false, nil
		}
		return "", time.Time{}, false, err
	}
	return path, capturedAt, true, nil
}

// DeleteAllFacePhotos removes every face photo (rows and files) for an employee.
// This is the only path that can leave an employee with no photo, and it is
// used only by HR's explicit delete. Returns how many photos were removed.
func DeleteAllFacePhotos(ctx context.Context, pool *pgxpool.Pool, uploadsDir string, employeeID pgtype.UUID) (int, error) {
	rows, err := pool.Query(ctx, `
		DELETE FROM employee_face_photos WHERE employee_id = $1 RETURNING photo_path`, employeeID)
	if err != nil {
		return 0, err
	}
	paths, err := collectPaths(rows)
	if err != nil {
		return 0, err
	}
	removeFiles(uploadsDir, paths)
	return len(paths), nil
}

// ListFacePhotoPaths returns the file names of all face photos for an employee.
// Used before deleting the employee: the rows cascade away with it, the files
// do not.
func ListFacePhotoPaths(ctx context.Context, pool *pgxpool.Pool, employeeID pgtype.UUID) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT photo_path FROM employee_face_photos WHERE employee_id = $1`, employeeID)
	if err != nil {
		return nil, err
	}
	return collectPaths(rows)
}

// RemoveFacePhotoFiles deletes face photo files from the uploads directory.
func RemoveFacePhotoFiles(uploadsDir string, paths []string) { removeFiles(uploadsDir, paths) }

// PurgeOldFacePhotos applies the retention rules to every employee. Returns how
// many photos were removed.
func PurgeOldFacePhotos(ctx context.Context, pool *pgxpool.Pool, uploadsDir string, now time.Time) (int, error) {
	return purgeFacePhotos(ctx, pool, uploadsDir, now, pgtype.UUID{})
}

func trimEmployeeFacePhotos(ctx context.Context, pool *pgxpool.Pool, uploadsDir string, employeeID pgtype.UUID) {
	if _, err := purgeFacePhotos(ctx, pool, uploadsDir, time.Now(), employeeID); err != nil {
		log.Printf("face photo: trim for employee failed: %v", err)
	}
}

// purgeFacePhotos deletes photos ranked after the newest (rn > 1) that are
// either past the retention window or beyond the per-employee cap. rn = 1, the
// newest photo, is never deleted. A valid employeeID limits it to that employee.
func purgeFacePhotos(ctx context.Context, pool *pgxpool.Pool, uploadsDir string, now time.Time, employeeID pgtype.UUID) (int, error) {
	rows, err := pool.Query(ctx, `
		WITH ranked AS (
		  SELECT id, captured_at,
		         row_number() OVER (PARTITION BY employee_id
		                            ORDER BY captured_at DESC, created_at DESC) AS rn
		  FROM employee_face_photos
		  WHERE ($3::uuid IS NULL OR employee_id = $3)
		)
		DELETE FROM employee_face_photos p
		USING ranked r
		WHERE p.id = r.id
		  AND r.rn > 1
		  AND (r.captured_at < $1 OR r.rn > $2)
		RETURNING p.photo_path`, now.Add(-FacePhotoRetention), FacePhotoMaxPerEmployee, employeeID)
	if err != nil {
		return 0, err
	}
	paths, err := collectPaths(rows)
	if err != nil {
		return 0, err
	}
	removeFiles(uploadsDir, paths)
	return len(paths), nil
}

// CleanupAttendancePhotos removes the per-check-in photos older server builds
// stored as attendance evidence (attendance_records.check_in_photo_path and the
// attendance-*.jpg files). Attendance records only keep times and the method.
//
// Before anything is deleted, an employee who has no face photo yet adopts
// their most recent attendance photo as one, so nobody who had a photo ends up
// without one. Safe to run repeatedly; once the old photos are gone it only
// scans the uploads directory. Returns how many files were deleted.
func CleanupAttendancePhotos(ctx context.Context, pool *pgxpool.Pool, uploadsDir string) (int, error) {
	// 1. Adopt: newest attendance photo per employee who has no face photo.
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT ON (ar.employee_id)
		       ar.employee_id, ar.check_in_photo_path,
		       COALESCE(ar.check_in, ar.date::timestamptz)
		FROM attendance_records ar
		WHERE ar.check_in_photo_path IS NOT NULL AND ar.check_in_photo_path <> ''
		  AND NOT EXISTS (SELECT 1 FROM employee_face_photos f WHERE f.employee_id = ar.employee_id)
		ORDER BY ar.employee_id, ar.date DESC, ar.check_in DESC NULLS LAST`)
	if err != nil {
		return 0, fmt.Errorf("find photos to adopt: %w", err)
	}
	type candidate struct {
		emp  pgtype.UUID
		path string
		at   time.Time
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.emp, &c.path, &c.at); err != nil {
			rows.Close()
			return 0, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, c := range cands {
		// Only adopt a file that is really there; a missing one would show as a
		// broken image on the profile.
		if fi, err := os.Stat(filepath.Join(uploadsDir, filepath.Base(c.path))); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if _, err := pool.Exec(ctx, insertFacePhotoSQL, c.emp, filepath.Base(c.path), FacePhotoSourceCheckin, c.at); err != nil {
			return 0, fmt.Errorf("adopt attendance photo: %w", err)
		}
	}

	// 2. Attendance records stop pointing at photos.
	if _, err := pool.Exec(ctx, `
		UPDATE attendance_records SET check_in_photo_path = NULL
		WHERE check_in_photo_path IS NOT NULL`); err != nil {
		return 0, fmt.Errorf("clear attendance photo paths: %w", err)
	}

	// 3. Delete every attendance-* file that is not an adopted face photo. Only
	// the device check-in endpoint ever created files with this prefix.
	files, err := filepath.Glob(filepath.Join(uploadsDir, "attendance-*"))
	if err != nil || len(files) == 0 {
		return 0, err
	}
	keepRows, err := pool.Query(ctx, `SELECT photo_path FROM employee_face_photos WHERE photo_path LIKE 'attendance-%'`)
	if err != nil {
		return 0, err
	}
	keepList, err := collectPaths(keepRows)
	if err != nil {
		return 0, err
	}
	keep := make(map[string]bool, len(keepList))
	for _, p := range keepList {
		keep[p] = true
	}
	removed := 0
	for _, f := range files {
		name := filepath.Base(f)
		if keep[name] || !strings.HasPrefix(name, "attendance-") {
			continue
		}
		if err := os.Remove(f); err != nil {
			log.Printf("attendance photo cleanup: remove %q failed: %v", name, err)
			continue
		}
		removed++
	}
	return removed, nil
}

const insertFacePhotoSQL = `
	INSERT INTO employee_face_photos (employee_id, photo_path, source, captured_at)
	VALUES ($1, $2, $3, $4)`

func writeFacePhotoFile(uploadsDir string, employeeID pgtype.UUID, src io.Reader) (string, error) {
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		return "", fmt.Errorf("mkdir uploads: %w", err)
	}
	fname := fmt.Sprintf("face-%s-%d.jpg", uuid.UUID(employeeID.Bytes).String(), time.Now().UnixNano())
	full := filepath.Join(uploadsDir, fname)
	dst, err := os.Create(full)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", fname, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(full)
		return "", fmt.Errorf("write %s: %w", fname, err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(full)
		return "", fmt.Errorf("close %s: %w", fname, err)
	}
	return fname, nil
}

type pathRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

func collectPaths(rows pathRows) ([]string, error) {
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func removeFiles(uploadsDir string, paths []string) {
	for _, p := range paths {
		if p == "" {
			continue
		}
		// filepath.Base guards against a stored value ever escaping uploadsDir.
		if err := os.Remove(filepath.Join(uploadsDir, filepath.Base(p))); err != nil && !os.IsNotExist(err) {
			log.Printf("face photo: remove %q failed: %v", p, err)
		}
	}
}
