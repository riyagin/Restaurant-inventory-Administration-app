// seed-face-photos gives employees in the LOCAL database a face photo, so the
// HR pages that show them (employee list, attendance overview, profile) can be
// checked without a phone. Real device photos go straight to the production
// server, so a local database otherwise has none.
//
// The pictures are drawn illustrations (head and shoulders on a coloured
// background), not photos of anyone, so they can't be mistaken for real data.
// Each employee always gets the same look (derived from their id).
//
// Who gets one: employees with attendance in the last 30 days. Every 8th is
// left out on purpose so the initial-letter fallback is visible too; -all
// includes them.
//
// Usage (from server-go/):
//
//	go run ./cmd/seed-face-photos          # add photos
//	go run ./cmd/seed-face-photos -all     # nobody left out
//	go run ./cmd/seed-face-photos -clean   # remove every seeded photo
//
// Re-running is safe: earlier seeded photos are removed first. Seeded files are
// named face-devseed-*.jpg and live in the normal uploads directory
// (UPLOADS_DIR, default ../server/uploads), served at /uploads/ by the dev API.
package main

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/config"
)

const seedPrefix = "face-devseed-"

func main() {
	clean := flag.Bool("clean", false, "only remove seeded photos")
	all := flag.Bool("all", false, "give every selected employee a photo (no initial-letter fallbacks)")
	flag.Parse()

	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.DBHost != "localhost" && cfg.DBHost != "127.0.0.1" {
		log.Fatalf("refusing to run: DB_HOST is %q; this tool is for a local database only", cfg.DBHost)
	}
	conn, err := pgx.Connect(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	removed, err := removeSeeded(ctx, conn, cfg.UploadsDir)
	if err != nil {
		log.Fatalf("clean: %v", err)
	}
	fmt.Printf("removed %d earlier seeded photo(s)\n", removed)
	if *clean {
		return
	}

	rows, err := conn.Query(ctx, `
		SELECT e.id, e.full_name
		FROM employees e
		WHERE EXISTS (
		  SELECT 1 FROM attendance_records ar
		  WHERE ar.employee_id = e.id AND ar.date >= CURRENT_DATE - 30
		)
		ORDER BY e.full_name`)
	if err != nil {
		log.Fatalf("list employees: %v", err)
	}
	type emp struct {
		id   pgtype.UUID
		name string
	}
	var emps []emp
	for rows.Next() {
		var e emp
		if err := rows.Scan(&e.id, &e.name); err != nil {
			log.Fatal(err)
		}
		emps = append(emps, e)
	}
	rows.Close()
	if len(emps) == 0 {
		fmt.Println("no employees with attendance in the last 30 days; nothing to do")
		return
	}

	if err := os.MkdirAll(cfg.UploadsDir, 0755); err != nil {
		log.Fatalf("uploads dir: %v", err)
	}

	added, skipped := 0, 0
	now := time.Now()
	for i, e := range emps {
		if !*all && i%8 == 7 {
			skipped++
			continue
		}
		id := uuid.UUID(e.id.Bytes)
		fname := fmt.Sprintf("%s%s.jpg", seedPrefix, id.String())
		if err := writePortrait(filepath.Join(cfg.UploadsDir, fname), id); err != nil {
			log.Fatalf("draw %s: %v", e.name, err)
		}
		// Captured "now" so it is each employee's latest photo; the hourly purge
		// always keeps the latest, so it stays until -clean or a newer real photo.
		if _, err := conn.Exec(ctx, `
			INSERT INTO employee_face_photos (employee_id, photo_path, source, captured_at)
			VALUES ($1, $2, 'checkin', $3)`, e.id, fname, now); err != nil {
			log.Fatalf("insert %s: %v", e.name, err)
		}
		added++
	}
	fmt.Printf("added %d photo(s) in %s", added, cfg.UploadsDir)
	if skipped > 0 {
		fmt.Printf("; left %d without one to show the initial-letter fallback", skipped)
	}
	fmt.Println()
}

func removeSeeded(ctx context.Context, conn *pgx.Conn, uploadsDir string) (int, error) {
	if _, err := conn.Exec(ctx, `DELETE FROM employee_face_photos WHERE photo_path LIKE $1`, seedPrefix+"%"); err != nil {
		return 0, err
	}
	files, _ := filepath.Glob(filepath.Join(uploadsDir, seedPrefix+"*.jpg"))
	for _, f := range files {
		_ = os.Remove(f)
	}
	return len(files), nil
}

// ── drawing ──────────────────────────────────────────────────────────────────

const (
	outW, outH = 240, 320
	ss         = 3 // supersampling factor, for smooth edges
)

var (
	backgrounds = []color.RGBA{
		{0xd9, 0xe7, 0xf7, 255}, {0xe3, 0xf1, 0xe4, 255}, {0xf6, 0xe9, 0xd7, 255},
		{0xee, 0xe3, 0xf5, 255}, {0xf7, 0xdf, 0xe2, 255}, {0xdf, 0xf0, 0xef, 255},
	}
	skins = []color.RGBA{
		{0xf1, 0xd0, 0xb3, 255}, {0xe0, 0xb4, 0x8e, 255}, {0xc9, 0x96, 0x6c, 255},
		{0xa8, 0x76, 0x4f, 255}, {0x8a, 0x5c, 0x3d, 255},
	}
	hairs = []color.RGBA{
		{0x1e, 0x1a, 0x18, 255}, {0x3b, 0x2a, 0x20, 255}, {0x5a, 0x3e, 0x2b, 255},
		{0x2a, 0x2a, 0x30, 255},
	}
	shirts = []color.RGBA{
		{0x2f, 0x5d, 0x9e, 255}, {0x2e, 0x7d, 0x32, 255}, {0xb2, 0x4a, 0x3a, 255},
		{0x5d, 0x4a, 0x8a, 255}, {0x37, 0x47, 0x4f, 255}, {0xc0, 0x85, 0x2a, 255},
	}
)

func writePortrait(path string, id uuid.UUID) error {
	h := fnv.New64a()
	h.Write(id[:])
	seed := h.Sum64()
	pick := func(n int) int {
		v := int(seed % uint64(n))
		seed /= uint64(n)
		return v
	}
	bg := backgrounds[pick(len(backgrounds))]
	skin := skins[pick(len(skins))]
	hair := hairs[pick(len(hairs))]
	shirt := shirts[pick(len(shirts))]
	longHair := pick(3) == 0
	glasses := pick(4) == 0

	W, H := outW*ss, outH*ss
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	fill(img, bg)
	f := func(v float64) float64 { return v * ss }

	// Long hair sits behind the head and shoulders.
	if longHair {
		ellipse(img, f(120), f(150), f(78), f(105), hair)
	}
	// Shoulders and neck.
	ellipse(img, f(120), f(345), f(118), f(95), shirt)
	ellipse(img, f(120), f(205), f(26), f(40), darker(skin, 0.92))
	// Head and ears.
	ellipse(img, f(64), f(140), f(10), f(16), skin)
	ellipse(img, f(176), f(140), f(10), f(16), skin)
	ellipse(img, f(120), f(135), f(56), f(70), skin)
	// Hair on top: a cap clipped to the upper part of the head.
	ellipseClipped(img, f(120), f(108), f(60), f(48), hair, f(118))
	// Eyes, brows, mouth.
	ellipse(img, f(98), f(140), f(5), f(5.5), color.RGBA{0x2b, 0x22, 0x1e, 255})
	ellipse(img, f(142), f(140), f(5), f(5.5), color.RGBA{0x2b, 0x22, 0x1e, 255})
	rect(img, f(88), f(124), f(108), f(128), darker(hair, 1))
	rect(img, f(132), f(124), f(152), f(128), darker(hair, 1))
	ellipseClipped(img, f(120), f(168), f(14), f(8), darker(skin, 0.6), -1)
	ellipseClipped(img, f(120), f(165), f(14), f(7), skin, -1)
	if glasses {
		ring(img, f(98), f(140), f(15), f(3), color.RGBA{0x30, 0x30, 0x30, 255})
		ring(img, f(142), f(140), f(15), f(3), color.RGBA{0x30, 0x30, 0x30, 255})
		rect(img, f(113), f(139), f(127), f(142), color.RGBA{0x30, 0x30, 0x30, 255})
	}

	out := downsample(img, ss)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return jpeg.Encode(file, out, &jpeg.Options{Quality: 85})
}

func fill(img *image.RGBA, c color.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
}

func ellipse(img *image.RGBA, cx, cy, rx, ry float64, c color.RGBA) {
	ellipseClipped(img, cx, cy, rx, ry, c, -1)
}

// ellipseClipped fills an ellipse; when maxY >= 0 only rows above maxY are drawn.
func ellipseClipped(img *image.RGBA, cx, cy, rx, ry float64, c color.RGBA, maxY float64) {
	b := img.Bounds()
	for y := int(cy - ry); y <= int(cy+ry); y++ {
		if y < b.Min.Y || y >= b.Max.Y || (maxY >= 0 && float64(y) > maxY) {
			continue
		}
		for x := int(cx - rx); x <= int(cx+rx); x++ {
			if x < b.Min.X || x >= b.Max.X {
				continue
			}
			dx, dy := (float64(x)-cx)/rx, (float64(y)-cy)/ry
			if dx*dx+dy*dy <= 1 {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func ring(img *image.RGBA, cx, cy, r, width float64, c color.RGBA) {
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d2 := dx*dx + dy*dy
			if d2 <= r*r && d2 >= (r-width)*(r-width) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func rect(img *image.RGBA, x0, y0, x1, y1 float64, c color.RGBA) {
	for y := int(y0); y < int(y1); y++ {
		for x := int(x0); x < int(x1); x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func darker(c color.RGBA, k float64) color.RGBA {
	return color.RGBA{uint8(float64(c.R) * k), uint8(float64(c.G) * k), uint8(float64(c.B) * k), 255}
}

// downsample averages k×k blocks (box filter), smoothing the drawn edges.
func downsample(src *image.RGBA, k int) *image.RGBA {
	w, h := src.Bounds().Dx()/k, src.Bounds().Dy()/k
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	n := uint32(k * k)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b uint32
			for yy := 0; yy < k; yy++ {
				for xx := 0; xx < k; xx++ {
					i := src.PixOffset(x*k+xx, y*k+yy)
					r += uint32(src.Pix[i])
					g += uint32(src.Pix[i+1])
					b += uint32(src.Pix[i+2])
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n), uint8(g / n), uint8(b / n), 255})
		}
	}
	return dst
}
