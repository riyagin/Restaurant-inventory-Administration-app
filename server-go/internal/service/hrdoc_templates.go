package service

import (
	"fmt"
	"strings"
)

// This file holds the actual legal text of the four templates. Wording is kept
// deliberately plain and standard-practice; it is a compliant skeleton, not a
// substitute for review by the company's own legal/HR counsel.

func heading(text string) Block   { return Block{Kind: kindHeading, Text: text} }
func para(text string) Block      { return Block{Kind: kindParagraph, Text: text} }
func clause(m, t string) Block    { return Block{Kind: kindClause, Marker: m, Text: t} }
func identity(rows []kv) Block     { return Block{Kind: kindIdentity, Items: rows} }
func title(t string) Block        { return Block{Kind: kindTitle, Text: t} }
func subtitle(t string) Block     { return Block{Kind: kindSubtitle, Text: t} }

func salaryText(in HRDocInput) string {
	period := strings.TrimSpace(in.SalaryPeriod)
	if period == "" {
		period = "bulan"
	}
	if in.Salary <= 0 {
		return fmt.Sprintf("sebesar Rp ……………………… (………………………) per %s", period)
	}
	return fmt.Sprintf("sebesar %s per %s", rupiahTerbilang(in.Salary), period)
}

func companyName(in HRDocInput) string {
	if n := strings.TrimSpace(in.CompanyName); n != "" {
		return n
	}
	return "Perusahaan"
}

// ---- PKWT (kontrak 1 tahun) ----------------------------------------------

func buildPKWT(in HRDocInput) *HRDocument {
	perusahaan := companyName(in)
	startTxt := dash(tanggalIndo(in.StartDate))
	endTxt := dash(tanggalIndo(in.EndDate))
	place := dash(in.PlaceOfWork)
	position := dash(in.EmployeePosition)

	pihak1 := []kv{
		{"Nama", dash(in.SignatoryName)},
		{"Jabatan", dash(in.SignatoryPosition)},
		{"Bertindak untuk dan atas nama", perusahaan},
	}
	if in.CompanyAddress != "" {
		pihak1 = append(pihak1, kv{"Alamat", in.CompanyAddress})
	}

	blocks := []Block{
		letterhead(in),
		spacer(4),
		title("PERJANJIAN KERJA WAKTU TERTENTU (PKWT)"),
	}
	if in.DocumentNumber != "" {
		blocks = append(blocks, subtitle("Nomor: "+in.DocumentNumber))
	}
	blocks = append(blocks,
		spacer(3),
		para(fmt.Sprintf("Pada hari ini, %s, yang bertanda tangan di bawah ini:",
			dash(hariTanggalIndo(in.DocumentDate)))),
		heading("I.  PIHAK PERTAMA (Pengusaha)"),
		identity(pihak1),
		para("Selanjutnya disebut sebagai PIHAK PERTAMA."),
		heading("II.  PIHAK KEDUA (Pekerja)"),
		identity(employeeIdentity(in)),
		para("Selanjutnya disebut sebagai PIHAK KEDUA."),
		spacer(2),
		para("PIHAK PERTAMA dan PIHAK KEDUA sepakat untuk mengadakan Perjanjian Kerja Waktu Tertentu (PKWT) yang tunduk pada Undang-Undang Nomor 13 Tahun 2003 tentang Ketenagakerjaan sebagaimana telah diubah dengan Undang-Undang Nomor 6 Tahun 2023 tentang Cipta Kerja beserta peraturan pelaksananya (Peraturan Pemerintah Nomor 35 Tahun 2021), dengan ketentuan sebagai berikut:"),

		heading("Pasal 1 — Jabatan dan Tempat Pekerjaan"),
		clause("1.", fmt.Sprintf("PIHAK PERTAMA mempekerjakan PIHAK KEDUA pada jabatan %s.", position)),
		clause("2.", fmt.Sprintf("Tempat pekerjaan PIHAK KEDUA adalah di %s.", place)),
		clause("3.", fmt.Sprintf("Uraian tugas dan tanggung jawab PIHAK KEDUA adalah: %s.", dash(in.JobDescription))),

		heading("Pasal 2 — Jangka Waktu Perjanjian"),
		clause("1.", fmt.Sprintf("Perjanjian kerja ini berlaku untuk jangka waktu tertentu, terhitung sejak tanggal %s sampai dengan tanggal %s.", startTxt, endTxt)),
		clause("2.", "Sesuai ketentuan Pasal 58 Undang-Undang Ketenagakerjaan, PKWT ini TIDAK mensyaratkan masa percobaan kerja (probation). Apabila disyaratkan, masa percobaan tersebut batal demi hukum."),
		clause("3.", "Perpanjangan perjanjian kerja, jika ada, dilakukan secara tertulis dengan tetap memperhatikan batas maksimal jangka waktu PKWT sebagaimana diatur dalam PP Nomor 35 Tahun 2021."),

		heading("Pasal 3 — Upah dan Cara Pembayaran"),
		clause("1.", fmt.Sprintf("PIHAK PERTAMA memberikan upah kepada PIHAK KEDUA %s.", salaryText(in))),
		clause("2.", fmt.Sprintf("Upah dibayarkan dengan cara: %s.", dash(in.PaymentInfo))),
		clause("3.", "Pemotongan dan pemungutan pajak atas penghasilan (PPh 21) serta iuran jaminan sosial dilakukan sesuai ketentuan peraturan perundang-undangan yang berlaku."),

		heading("Pasal 4 — Waktu Kerja, Hak, dan Kewajiban"),
		clause("1.", fmt.Sprintf("Waktu kerja PIHAK KEDUA mengikuti ketentuan: %s, sesuai dengan peraturan perusahaan dan ketentuan waktu kerja dalam peraturan perundang-undangan.", dash(in.WorkingHours))),
		clause("2.", "PIHAK KEDUA berhak atas hari istirahat, cuti, serta perlindungan Jaminan Sosial (BPJS Ketenagakerjaan dan BPJS Kesehatan) sesuai ketentuan yang berlaku."),
		clause("3.", "PIHAK KEDUA wajib mematuhi peraturan perusahaan, menjaga rahasia perusahaan, serta melaksanakan tugas dengan penuh tanggung jawab."),

		heading("Pasal 5 — Uang Kompensasi"),
		clause("1.", "Sesuai Pasal 15 dan Pasal 16 Peraturan Pemerintah Nomor 35 Tahun 2021, pada saat berakhirnya PKWT, PIHAK PERTAMA wajib memberikan uang kompensasi kepada PIHAK KEDUA yang telah mempunyai masa kerja paling sedikit 1 (satu) bulan secara terus-menerus."),
		clause("2.", "Besaran uang kompensasi dihitung secara proporsional terhadap masa kerja, di mana masa kerja 12 (dua belas) bulan secara terus-menerus setara dengan 1 (satu) kali upah sebulan."),

		heading("Pasal 6 — Berakhirnya Perjanjian Kerja"),
		clause("1.", "Perjanjian kerja berakhir dengan sendirinya pada saat berakhirnya jangka waktu sebagaimana diatur dalam Pasal 2."),
		clause("2.", "Dalam hal salah satu pihak mengakhiri hubungan kerja sebelum jangka waktu berakhir, berlaku ketentuan ganti rugi dan hak-hak sesuai peraturan perundang-undangan yang berlaku."),

		heading("Pasal 7 — Penyelesaian Perselisihan"),
		para("Segala perselisihan yang timbul dari perjanjian ini diselesaikan terlebih dahulu secara musyawarah untuk mufakat. Apabila tidak tercapai, penyelesaian dilakukan melalui mekanisme Penyelesaian Perselisihan Hubungan Industrial sesuai Undang-Undang Nomor 2 Tahun 2004."),

		heading("Pasal 8 — Penutup"),
		para("Perjanjian ini dibuat dalam rangkap 2 (dua) bermaterai cukup, masing-masing mempunyai kekuatan hukum yang sama, dan ditandatangani oleh kedua belah pihak dalam keadaan sadar tanpa paksaan dari pihak manapun."),
		spacer(4),
		twoPartySignature(in),
	)

	return &HRDocument{FilenameBase: filenameFor("PKWT", in.EmployeeName), Blocks: blocks}
}

// ---- PKWTT (karyawan tetap) ----------------------------------------------

func buildPKWTT(in HRDocInput) *HRDocument {
	perusahaan := companyName(in)
	startTxt := dash(tanggalIndo(in.StartDate))
	place := dash(in.PlaceOfWork)
	position := dash(in.EmployeePosition)

	pihak1 := []kv{
		{"Nama", dash(in.SignatoryName)},
		{"Jabatan", dash(in.SignatoryPosition)},
		{"Bertindak untuk dan atas nama", perusahaan},
	}
	if in.CompanyAddress != "" {
		pihak1 = append(pihak1, kv{"Alamat", in.CompanyAddress})
	}

	probationClause := "Para pihak tidak menetapkan masa percobaan kerja."
	if in.ProbationMonths > 0 {
		m := in.ProbationMonths
		if m > 3 {
			m = 3
		}
		probationClause = fmt.Sprintf("PIHAK KEDUA menjalani masa percobaan kerja selama %d (………) bulan terhitung sejak tanggal mulai bekerja, dengan ketentuan paling lama 3 (tiga) bulan sesuai Pasal 60 Undang-Undang Ketenagakerjaan. Selama masa percobaan, PIHAK PERTAMA dilarang membayar upah di bawah upah minimum yang berlaku.", m)
	}

	blocks := []Block{
		letterhead(in),
		spacer(4),
		title("PERJANJIAN KERJA WAKTU TIDAK TERTENTU (PKWTT)"),
	}
	if in.DocumentNumber != "" {
		blocks = append(blocks, subtitle("Nomor: "+in.DocumentNumber))
	}
	blocks = append(blocks,
		spacer(3),
		para(fmt.Sprintf("Pada hari ini, %s, yang bertanda tangan di bawah ini:",
			dash(hariTanggalIndo(in.DocumentDate)))),
		heading("I.  PIHAK PERTAMA (Pengusaha)"),
		identity(pihak1),
		para("Selanjutnya disebut sebagai PIHAK PERTAMA."),
		heading("II.  PIHAK KEDUA (Pekerja)"),
		identity(employeeIdentity(in)),
		para("Selanjutnya disebut sebagai PIHAK KEDUA."),
		spacer(2),
		para("Kedua belah pihak sepakat untuk mengadakan Perjanjian Kerja Waktu Tidak Tertentu (PKWTT) sebagai hubungan kerja untuk pekerja tetap, yang tunduk pada Undang-Undang Nomor 13 Tahun 2003 tentang Ketenagakerjaan sebagaimana telah diubah dengan Undang-Undang Nomor 6 Tahun 2023 tentang Cipta Kerja beserta peraturan pelaksananya, dengan ketentuan sebagai berikut:"),

		heading("Pasal 1 — Jabatan dan Tempat Pekerjaan"),
		clause("1.", fmt.Sprintf("PIHAK PERTAMA mengangkat PIHAK KEDUA sebagai pekerja tetap dengan jabatan %s.", position)),
		clause("2.", fmt.Sprintf("Tempat pekerjaan PIHAK KEDUA adalah di %s.", place)),
		clause("3.", fmt.Sprintf("Uraian tugas dan tanggung jawab PIHAK KEDUA adalah: %s.", dash(in.JobDescription))),

		heading("Pasal 2 — Mulai Berlaku dan Masa Percobaan"),
		clause("1.", fmt.Sprintf("Hubungan kerja ini mulai berlaku terhitung sejak tanggal %s dan berlaku untuk waktu tidak tertentu.", startTxt)),
		clause("2.", probationClause),
		clause("3.", "Hubungan kerja berlangsung sampai dengan PIHAK KEDUA memasuki usia pensiun atau berakhir karena sebab-sebab lain sesuai peraturan perundang-undangan."),

		heading("Pasal 3 — Upah dan Cara Pembayaran"),
		clause("1.", fmt.Sprintf("PIHAK PERTAMA memberikan upah kepada PIHAK KEDUA %s.", salaryText(in))),
		clause("2.", fmt.Sprintf("Upah dibayarkan dengan cara: %s.", dash(in.PaymentInfo))),
		clause("3.", "Peninjauan upah dilakukan secara berkala sesuai kebijakan perusahaan dan ketentuan upah minimum yang berlaku."),

		heading("Pasal 4 — Waktu Kerja, Hak, dan Kewajiban"),
		clause("1.", fmt.Sprintf("Waktu kerja PIHAK KEDUA mengikuti ketentuan: %s, sesuai peraturan perusahaan dan peraturan perundang-undangan.", dash(in.WorkingHours))),
		clause("2.", "PIHAK KEDUA berhak atas cuti tahunan, hari istirahat, Tunjangan Hari Raya (THR) keagamaan, serta kepesertaan BPJS Ketenagakerjaan dan BPJS Kesehatan sesuai ketentuan yang berlaku."),
		clause("3.", "PIHAK KEDUA wajib menaati peraturan perusahaan, menjaga nama baik dan rahasia perusahaan, serta melaksanakan tugas dengan penuh tanggung jawab."),

		heading("Pasal 5 — Pemutusan Hubungan Kerja"),
		clause("1.", "Pemutusan Hubungan Kerja (PHK) hanya dapat dilakukan sesuai alasan dan mekanisme yang diatur dalam peraturan perundang-undangan."),
		clause("2.", "Dalam hal terjadi PHK, PIHAK KEDUA berhak atas uang pesangon, uang penghargaan masa kerja, dan/atau uang penggantian hak sesuai ketentuan Peraturan Pemerintah Nomor 35 Tahun 2021."),

		heading("Pasal 6 — Penyelesaian Perselisihan"),
		para("Segala perselisihan yang timbul dari perjanjian ini diselesaikan secara musyawarah untuk mufakat. Apabila tidak tercapai, penyelesaian dilakukan melalui mekanisme Penyelesaian Perselisihan Hubungan Industrial sesuai Undang-Undang Nomor 2 Tahun 2004."),

		heading("Pasal 7 — Penutup"),
		para("Perjanjian ini dibuat dalam rangkap 2 (dua) bermaterai cukup, masing-masing mempunyai kekuatan hukum yang sama, dan ditandatangani oleh kedua belah pihak dalam keadaan sadar tanpa paksaan dari pihak manapun."),
		spacer(4),
		twoPartySignature(in),
	)

	return &HRDocument{FilenameBase: filenameFor("PKWTT", in.EmployeeName), Blocks: blocks}
}

// ---- Surat Peringatan (SP) -----------------------------------------------

func buildSuratPeringatan(in HRDocInput) *HRDocument {
	level := strings.TrimSpace(in.WarningLevel)
	roman := map[string]string{"1": "PERTAMA (SP-1)", "2": "KEDUA (SP-2)", "3": "KETIGA (SP-3)"}[level]
	if roman == "" {
		roman = "PERTAMA (SP-1)"
		level = "1"
	}
	validity := in.ValidityMonths
	if validity <= 0 {
		validity = 6
	}

	blocks := []Block{
		letterhead(in),
		spacer(4),
		title("SURAT PERINGATAN " + roman),
	}
	if in.DocumentNumber != "" {
		blocks = append(blocks, subtitle("Nomor: "+in.DocumentNumber))
	}
	blocks = append(blocks,
		spacer(3),
		para(fmt.Sprintf("Manajemen %s dengan ini memberikan Surat Peringatan kepada karyawan:", companyName(in))),
		identity(employeeIdentity(in)),
		spacer(1),
	)

	// Dasar / kronologi.
	if level == "2" || level == "3" {
		prev := dash(in.PreviousWarningRef)
		blocks = append(blocks, para(fmt.Sprintf("Surat Peringatan ini diterbitkan sebagai kelanjutan dari peringatan sebelumnya (%s), karena karyawan yang bersangkutan kembali melakukan pelanggaran dalam masa berlakunya peringatan tersebut.", prev)))
	}
	blocks = append(blocks,
		para(fmt.Sprintf("Berdasarkan hasil pemeriksaan, karyawan yang bersangkutan terbukti melakukan pelanggaran terhadap ketentuan dalam perjanjian kerja dan/atau peraturan perusahaan, dengan uraian sebagai berikut:")),
		clause("a.", fmt.Sprintf("Jenis pelanggaran: %s.", dash(in.ViolationDetail))),
		clause("b.", fmt.Sprintf("Terjadi pada tanggal: %s.", dash(tanggalIndo(in.ViolationDate)))),
		spacer(1),
		para(fmt.Sprintf("Sesuai Pasal 154A Undang-Undang Nomor 6 Tahun 2023 (Cipta Kerja) juncto Peraturan Pemerintah Nomor 35 Tahun 2021, Surat Peringatan ini berlaku untuk jangka waktu paling lama %d (…………) bulan terhitung sejak tanggal diterbitkan.", validity)),
		para(fmt.Sprintf("Apabila dalam masa berlaku peringatan ini karyawan kembali melakukan pelanggaran, maka perusahaan dapat menjatuhkan sanksi yang lebih berat berupa: %s.", dash(in.Consequence))),
		para(fmt.Sprintf("Kami mengharapkan karyawan yang bersangkutan untuk memperbaiki diri, khususnya dalam hal: %s.", dash(in.ImprovementExpected))),
		spacer(2),
		para("Demikian Surat Peringatan ini dibuat untuk dilaksanakan dan menjadi perhatian sebagaimana mestinya."),
		spacer(4),
		acknowledgeSignature(in),
	)

	return &HRDocument{FilenameBase: filenameFor("Surat-Peringatan-"+level, in.EmployeeName), Blocks: blocks}
}

// ---- Paklaring (surat pengalaman kerja) ----------------------------------

func buildPaklaring(in HRDocInput) *HRDocument {
	perusahaan := companyName(in)
	position := dash(in.EmployeePosition)
	startTxt := dash(tanggalIndo(in.StartDate))
	endTxt := dash(tanggalIndo(in.EndDate))

	signer := []kv{
		{"Nama", dash(in.SignatoryName)},
		{"Jabatan", dash(in.SignatoryPosition)},
	}

	conduct := strings.TrimSpace(in.ConductNote)
	if conduct == "" {
		conduct = "Selama bekerja, yang bersangkutan telah menunjukkan dedikasi, kejujuran, dan tanggung jawab yang baik dalam melaksanakan tugas dan kewajibannya."
	}

	blocks := []Block{
		letterhead(in),
		spacer(4),
		title("SURAT KETERANGAN PENGALAMAN KERJA"),
		subtitle("(PAKLARING)"),
	}
	if in.DocumentNumber != "" {
		blocks = append(blocks, subtitle("Nomor: "+in.DocumentNumber))
	}
	blocks = append(blocks,
		spacer(3),
		para("Yang bertanda tangan di bawah ini:"),
		identity(signer),
		para(fmt.Sprintf("Dalam hal ini bertindak untuk dan atas nama %s, dengan ini menerangkan bahwa:", perusahaan)),
		identity(employeeIdentity(in)),
		spacer(1),
		para(fmt.Sprintf("Adalah benar karyawan %s yang telah bekerja dengan jabatan terakhir sebagai %s, terhitung sejak tanggal %s sampai dengan tanggal %s.", perusahaan, position, startTxt, endTxt)),
		para(conduct),
	)
	if strings.TrimSpace(in.ReasonLeaving) != "" {
		blocks = append(blocks, para(fmt.Sprintf("Yang bersangkutan mengakhiri hubungan kerja dengan alasan: %s. Pemutusan hubungan kerja berlangsung secara baik-baik tanpa meninggalkan kewajiban apapun terhadap perusahaan.", strings.TrimSpace(in.ReasonLeaving))))
	}
	blocks = append(blocks,
		para("Selama masa kerjanya, yang bersangkutan tidak memiliki ikatan maupun tanggungan yang belum diselesaikan dengan perusahaan."),
		para("Demikian surat keterangan ini dibuat dengan sebenarnya agar dapat dipergunakan sebagaimana mestinya. Kepada yang bersangkutan kami ucapkan terima kasih dan selamat menempuh karier selanjutnya."),
		spacer(5),
		singleSignature(in),
	)

	return &HRDocument{FilenameBase: filenameFor("Paklaring", in.EmployeeName), Blocks: blocks}
}

// ---- signature block builders --------------------------------------------

func twoPartySignature(in HRDocInput) Block {
	return Block{Kind: kindSignature, Cols: []signCol{
		{
			TopLines: []string{""},
			Role:     "PIHAK PERTAMA",
			Name:     dash(in.SignatoryName),
			SubLines: []string{dash(in.SignatoryPosition), companyName(in)},
		},
		{
			TopLines: []string{signPlaceDate(in), "Materai Rp10.000"},
			Role:     "PIHAK KEDUA",
			Name:     dash(in.EmployeeName),
			SubLines: []string{"Pekerja"},
		},
	}}
}

func singleSignature(in HRDocInput) Block {
	return Block{Kind: kindSignature, Cols: []signCol{
		{}, // empty left column to push the signature to the right
		{
			TopLines: []string{signPlaceDate(in), "Hormat kami,", companyName(in)},
			Name:     dash(in.SignatoryName),
			SubLines: []string{dash(in.SignatoryPosition)},
		},
	}}
}

// acknowledgeSignature puts the employee's acknowledgment (Diterima oleh) on the
// left and the issuing manager on the right — standard for a Surat Peringatan.
func acknowledgeSignature(in HRDocInput) Block {
	return Block{Kind: kindSignature, Cols: []signCol{
		{
			TopLines: []string{"Diterima oleh,", "Karyawan"},
			Name:     dash(in.EmployeeName),
			SubLines: []string{""},
		},
		{
			TopLines: []string{signPlaceDate(in), "Hormat kami,", companyName(in)},
			Name:     dash(in.SignatoryName),
			SubLines: []string{dash(in.SignatoryPosition)},
		},
	}}
}

// filenameFor builds a safe filename base like "PKWT-Budi-Santoso".
func filenameFor(kind, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Karyawan"
	}
	repl := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-", "*", "", "?", "", "\"", "", "<", "", ">", "", "|", "")
	return kind + "-" + repl.Replace(name)
}
