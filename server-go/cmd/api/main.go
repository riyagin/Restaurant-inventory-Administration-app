package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
	"inventory-app/server-go/internal/handler"
	appmiddleware "inventory-app/server-go/internal/middleware"
	"inventory-app/server-go/internal/service"
)

// formatUptime renders a duration in Indonesian, coarsest unit first (e.g.
// "2 jam 5 menit"). Seconds are only shown for uptimes under a minute, where
// they are the only meaningful figure.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d detik", int(d.Seconds()))
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d hari", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d jam", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d menit", minutes))
	}
	return strings.Join(parts, " ")
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()
	log.Println("database connected")

	m, err := migrate.New("file://migrations", cfg.DBUrl)
	if err != nil {
		log.Fatalf("failed to init migrations: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to run migrations: %v", err)
	}
	log.Println("migrations up to date")

	queries := db.New(pool)

	// Record the restart in the activity log. Logged here — after the pool and
	// migrations are up, before the listener starts — so the entry exists even if
	// binding the port then fails.
	//
	// A crash (SIGKILL, OOM, power loss) cannot write its own "stop" row, so a
	// start with no preceding stop is itself the signal that the process died
	// rather than being shut down. Uses its own context: this must not be tied to
	// the signal context that shutdown cancels.
	startedAt := time.Now()
	logCtx, logCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := service.LogSystemEvent(logCtx, queries, service.ActionServerStart,
		fmt.Sprintf("Server dijalankan pada port %s", cfg.Port)); err != nil {
		// Never fatal: an audit row is not worth refusing to serve traffic over.
		log.Printf("activity log: failed to record server start: %v", err)
	}
	logCancel()

	// Token cleanup goroutine
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := queries.DeleteExpiredTokens(context.Background()); err != nil {
					log.Printf("token cleanup error: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Nightly attendance reconciliation goroutine. Mirrors the token-cleanup
	// pattern (time.Ticker). Every 24h it inserts 'absent' rows for the previous
	// day for scheduled work days with no record (skipping holidays/non-work
	// days). The same service.ReconcileAbsent function backs the manual
	// POST /api/hr/attendance/reconcile endpoint.
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				yesterday := time.Now().AddDate(0, 0, -1)
				bg := context.Background()
				if _, err := service.ReconcileAbsent(bg, queries, yesterday); err != nil {
					log.Printf("attendance reconcile error: %v", err)
				} else {
					log.Printf("attendance reconcile completed for %s", yesterday.Format("2006-01-02"))
				}
				// After absent rows are inserted, evaluate performance for the same
				// day so violations/scores reflect the now-finalized records.
				if err := service.EvaluateDay(bg, queries, yesterday); err != nil {
					log.Printf("performance evaluation error: %v", err)
				} else {
					log.Printf("performance evaluation completed for %s", yesterday.Format("2006-01-02"))
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	authHandler := handler.NewAuthHandler(queries, cfg.JWTSecret)
	usersHandler := handler.NewUsersHandler(queries)
	warehousesHandler := handler.NewWarehousesHandler(pool, queries)
	vendorsHandler := handler.NewVendorsHandler(pool, queries)
	itemsHandler := handler.NewItemsHandler(pool, queries)
	accountsHandler := handler.NewAccountsHandler(pool, queries)
	templatesHandler := handler.NewInvoiceTemplatesHandler(pool, queries)
	branchesHandler := handler.NewBranchesHandler(pool, queries)
	divisionsHandler := handler.NewDivisionsHandler(pool, queries)
	expenseCategoriesHandler := handler.NewExpenseCategoriesHandler(pool, queries)
	inventoryHandler := handler.NewInventoryHandler(pool, queries)
	stockHistoryHandler := handler.NewStockHistoryHandler(queries)
	stockTransfersHandler := handler.NewStockTransfersHandler(pool, queries)
	stockOpnameHandler := handler.NewStockOpnameHandler(pool, queries)
	invoicesHandler := handler.NewInvoicesHandler(pool, queries)
	invoicesHandler.SetUploadsDir(cfg.UploadsDir)
	dailyPurchasesHandler := handler.NewDailyPurchasesHandler(pool, queries)
	dailyPurchasesHandler.SetUploadsDir(cfg.UploadsDir)
	pettyCashHandler := handler.NewPettyCashHandler(pool, queries)
	cashTrackingHandler := handler.NewCashTrackingHandler(pool, queries)
	cashDepositsHandler := handler.NewCashDepositsHandler(pool, queries)
	dispatchesHandler := handler.NewDispatchesHandler(pool, queries)
	dispatchTemplatesHandler := handler.NewDispatchTemplatesHandler(pool, queries)
	enumerationsHandler := handler.NewEnumerationsHandler(pool, queries)
	recipesHandler := handler.NewRecipesHandler(pool, queries)
	productionsHandler := handler.NewProductionsHandler(pool, queries)
	salesHandler := handler.NewSalesHandler(pool, queries)
	posImportHandler := handler.NewPOSImportHandler(pool, queries)
	activityLogHandler := handler.NewActivityLogHandler(queries)
	adjustmentsHandler := handler.NewAccountAdjustmentsHandler(pool, queries)
	reportsHandler := handler.NewReportsHandler(pool, queries)
	analyticsHandler := handler.NewAnalyticsHandler(pool, queries)
	statsHandler := handler.NewStatsHandler(pool)
	tasksHandler := handler.NewTasksHandler(pool, queries)
	hrEmployeesHandler := handler.NewHREmployeesHandler(pool, queries)
	hrEmployeesHandler.SetUploadsDir(cfg.UploadsDir)
	hrWagesHandler := handler.NewHRWagesHandler(pool, queries)
	hrImportHandler := handler.NewHRImportHandler(pool, queries)
	attendanceHandler := handler.NewAttendanceHandler(pool, queries)
	attendanceDeviceHandler := handler.NewAttendanceDeviceHandler(pool, queries)
	attendanceDeviceHandler.SetUploadsDir(cfg.UploadsDir)
	performanceHandler := handler.NewPerformanceHandler(pool, queries)
	leaveHandler := handler.NewLeaveHandler(pool, queries)
	kasbonHandler := handler.NewKasbonHandler(pool, queries)
	kasbonHandler.SetUploadsDir(cfg.UploadsDir)
	payrollHandler := handler.NewPayrollHandler(pool, queries)
	// Retry sweep for payroll ledger postings. Closing a period queues a
	// payroll_postings row and fires the posting in the background; this catches
	// anything that failed or was interrupted by a restart, so a closed period
	// always ends up in the journal without anyone pressing a button.
	payrollHandler.Poster().Start(ctx)
	overtimeHandler := handler.NewOvertimeHandler(pool, queries)
	payslipHandler := handler.NewPayslipHandler(pool, queries)
	payslipHandler.SetUploadsDir(cfg.UploadsDir)
	thrHandler := handler.NewThrHandler(pool, queries)
	thrHandler.SetUploadsDir(cfg.UploadsDir)
	hrDocumentsHandler := handler.NewHRDocumentsHandler(pool, queries)
	hrDocumentsHandler.SetUploadsDir(cfg.UploadsDir)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: false,
		MaxAge:           300,
	}))
	r.Use(appmiddleware.APIRateLimit())

	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "db": "unreachable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "db": "connected"})
	})

	// Uploaded receipts, employee photos and the company logo.
	//
	// In production nginx serves this location directly and never reaches Go, so
	// this route existing changes nothing there. In development nothing served it
	// at all: every <img src="…/uploads/…"> 404'd, which looked exactly like a
	// broken upload and is the likeliest thing anyone reporting "photos don't
	// work" has actually been seeing.
	//
	// Deliberately unauthenticated, matching nginx: these URLs are already public
	// in production, and putting a JWT gate on only the dev path would hide the
	// difference rather than fix it. http.Dir refuses paths escaping the root, so
	// a crafted ../ cannot walk out of the uploads directory.
	r.Handle("/uploads/*", http.StripPrefix("/uploads/",
		http.FileServer(http.Dir(cfg.UploadsDir))))

	// Public auth routes
	r.With(appmiddleware.LoginRateLimit()).Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/refresh", authHandler.Refresh)

	// Device API — NOT JWT. Authenticated by X-Device-Key, rate-limited per key.
	// Lives OUTSIDE the JWT group so the Android face app can push events.
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.DeviceAuth(queries))
		r.Post("/api/hr/attendance/device/event", attendanceDeviceHandler.Event)
		r.Get("/api/hr/attendance/device/employees", attendanceDeviceHandler.Employees)
		r.Post("/api/hr/attendance/device/face", attendanceDeviceHandler.EnrollFace)
		r.Post("/api/hr/attendance/device/face/sync", attendanceDeviceHandler.FaceSync)
	})

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.Authenticate(queries, cfg.JWTSecret))
		// The HR-only role is confined to /api/hr/* (plus the shared master data
		// its screens read). Applied once here rather than per-route, because most
		// routes below are deliberately open to every authenticated user.
		r.Use(appmiddleware.RestrictHRRole)
		r.Post("/api/auth/logout", authHandler.Logout)

		// Users — all authenticated
		r.Get("/api/users", usersHandler.List)
		r.Post("/api/users", usersHandler.Create)
		r.Put("/api/users/{id}", usersHandler.Update)
		r.Delete("/api/users/{id}", usersHandler.Delete)

		// Warehouses — all authenticated
		r.Get("/api/warehouses", warehousesHandler.List)
		r.Post("/api/warehouses", warehousesHandler.Create)
		r.Put("/api/warehouses/{id}", warehousesHandler.Update)
		r.Delete("/api/warehouses/{id}", warehousesHandler.Delete)

		// Vendors — all authenticated
		r.Get("/api/vendors", vendorsHandler.List)
		r.Get("/api/vendors/{id}/history", vendorsHandler.GetHistory)
		r.Post("/api/vendors", vendorsHandler.Create)
		r.Put("/api/vendors/{id}", vendorsHandler.Update)
		r.Delete("/api/vendors/{id}", vendorsHandler.Delete)

		// Vendor transfer accounts — a vendor may be paid to several numbers
		r.Get("/api/vendors/{id}/bank-accounts", vendorsHandler.ListBankAccounts)
		r.Post("/api/vendors/{id}/bank-accounts", vendorsHandler.CreateBankAccount)
		r.Put("/api/vendors/{id}/bank-accounts/{bankId}", vendorsHandler.UpdateBankAccount)
		r.Delete("/api/vendors/{id}/bank-accounts/{bankId}", vendorsHandler.DeleteBankAccount)

		// Items — all authenticated
		r.Get("/api/items", itemsHandler.List)
		r.Get("/api/items/{id}", itemsHandler.Get)
		r.Get("/api/items/{id}/last-price", itemsHandler.GetLastPrice)
		r.Get("/api/items/{id}/history", itemsHandler.GetHistory)
		r.Get("/api/items/{id}/stock-history", itemsHandler.GetStockHistory)
		r.Get("/api/items/{id}/stock-detail", itemsHandler.GetStockDetail)
		r.Get("/api/items/{id}/price-history", itemsHandler.GetPriceHistory)
		r.Post("/api/items", itemsHandler.Create)
		r.Put("/api/items/{id}", itemsHandler.Update)
		r.Delete("/api/items/{id}", itemsHandler.Delete)

		// Accounts — all authenticated
		r.Get("/api/accounts", accountsHandler.List)
		r.Get("/api/accounts/trial-balance", accountsHandler.TrialBalance)
		r.Get("/api/accounts/cash-reconciliation", accountsHandler.CashReconciliation)
		r.Post("/api/accounts", accountsHandler.Create)
		r.Put("/api/accounts/{id}", accountsHandler.Update)
		r.Delete("/api/accounts/{id}", accountsHandler.Delete)
		// Owner capital deposits — the supported way to put money into the
		// business, including cash carried over from a previous system. Admin
		// only: it moves equity.
		r.With(appmiddleware.RequireCoreAccess).Post("/api/accounts/capital-injection", accountsHandler.CapitalInjection)

		// Invoice Templates — all authenticated
		r.Get("/api/invoice-templates", templatesHandler.List)
		r.Get("/api/invoice-templates/{id}", templatesHandler.Get)
		r.Post("/api/invoice-templates", templatesHandler.Create)
		r.Put("/api/invoice-templates/{id}", templatesHandler.Update)
		r.Delete("/api/invoice-templates/{id}", templatesHandler.Delete)

		// Branches — all authenticated
		r.Get("/api/branches", branchesHandler.List)
		r.Get("/api/branches/{id}", branchesHandler.Get)
		r.Post("/api/branches", branchesHandler.Create)
		r.Put("/api/branches/{id}", branchesHandler.Update)
		r.Delete("/api/branches/{id}", branchesHandler.Delete)

		// Divisions — all authenticated
		r.Get("/api/divisions", divisionsHandler.List)
		r.Post("/api/divisions", divisionsHandler.Create)
		r.Put("/api/divisions/{id}", divisionsHandler.Update)
		r.Delete("/api/divisions/{id}", divisionsHandler.Delete)
		r.Get("/api/division-categories", divisionsHandler.ListCategories)
		r.Post("/api/division-categories", divisionsHandler.CreateCategory)
		r.Delete("/api/division-categories/{id}", divisionsHandler.DeleteCategory)

		// Expense categories — the subaccount level under a division's expense
		// account. Distinct from division-categories above, which are the POS
		// import's revenue labels.
		r.Get("/api/expense-categories", expenseCategoriesHandler.List)
		r.Post("/api/expense-categories", expenseCategoriesHandler.Create)
		r.Delete("/api/expense-categories/{id}", expenseCategoriesHandler.Delete)

		// Inventory — all authenticated
		r.Get("/api/inventory", inventoryHandler.List)
		r.Get("/api/inventory/count", inventoryHandler.Count)
		// Registered before /{id} so "lots" is not swallowed as an ID.
		r.Get("/api/inventory/lots/{id}/history", inventoryHandler.LotHistory)
		r.Get("/api/inventory/{id}", inventoryHandler.Get)
		r.Post("/api/inventory", inventoryHandler.Create)
		r.Put("/api/inventory/{id}", inventoryHandler.Update)
		r.Delete("/api/inventory/{id}", inventoryHandler.Delete)

		// Stock History — all authenticated
		r.Get("/api/stock-history/{itemId}", stockHistoryHandler.List)

		// Stock Transfers — all authenticated
		r.Get("/api/stock-transfers", stockTransfersHandler.List)
		r.Get("/api/stock-transfers/group/{groupId}", stockTransfersHandler.ListByGroup)
		r.Post("/api/stock-transfers", stockTransfersHandler.Create)
		r.Put("/api/stock-transfers/group/{groupId}", stockTransfersHandler.Update)
		r.Delete("/api/stock-transfers/group/{groupId}", stockTransfersHandler.Delete)

		// Stock Opname — all authenticated
		r.Get("/api/stock-opname", stockOpnameHandler.List)
		r.Post("/api/stock-opname", stockOpnameHandler.Create)
		r.Get("/api/stock-opname/drafts", stockOpnameHandler.ListDrafts)
		r.Post("/api/stock-opname/drafts", stockOpnameHandler.SaveDraft)
		r.Put("/api/stock-opname/drafts/{id}", stockOpnameHandler.UpdateDraft)
		r.Delete("/api/stock-opname/drafts/{id}", stockOpnameHandler.DeleteDraft)
		r.Get("/api/stock-opname/{id}", stockOpnameHandler.Get)
		r.Put("/api/stock-opname/{id}", stockOpnameHandler.Update)

		// Invoices — all authenticated; delete admin only
		r.Get("/api/invoices", invoicesHandler.List)
		r.Get("/api/invoices/{id}", invoicesHandler.Get)
		r.Post("/api/invoices", invoicesHandler.Create)
		r.Put("/api/invoices/{id}", invoicesHandler.Update)
		r.Post("/api/invoices/{id}/pay", invoicesHandler.Pay)
		r.Post("/api/invoices/{id}/photo", invoicesHandler.UploadPhoto)
		r.Delete("/api/invoices/{id}/photo", invoicesHandler.DeletePhoto)
		r.Delete("/api/invoices/{id}", invoicesHandler.Delete)

		// Pembelanjaan Harian — recording a spend is the same reach as recording
		// an invoice, so admin and staff both do it. Cancelling reverses stock and
		// the ledger, which is an admin decision.
		r.Get("/api/daily-purchases", dailyPurchasesHandler.List)
		r.Get("/api/daily-purchases/{id}", dailyPurchasesHandler.Get)
		r.Post("/api/daily-purchases", dailyPurchasesHandler.Create)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/daily-purchases/{id}/cancel", dailyPurchasesHandler.Cancel)
		})

		// Templates for the repeating shopping runs. Same reach as the purchases
		// themselves — whoever records the Tuesday market list should be able to
		// fix the list when it changes.
		r.Get("/api/daily-purchase-templates", dailyPurchasesHandler.ListTemplates)
		r.Post("/api/daily-purchase-templates", dailyPurchasesHandler.CreateTemplate)
		r.Put("/api/daily-purchase-templates/{id}", dailyPurchasesHandler.UpdateTemplate)
		r.Delete("/api/daily-purchase-templates/{id}", dailyPurchasesHandler.DeleteTemplate)

		// Petty cash — everyone can see the board (staff need to know what is in
		// the box before they spend from it), but only admin records the counts.
		r.Get("/api/petty-cash", pettyCashHandler.Day)
		r.Get("/api/petty-cash/history", pettyCashHandler.History)
		r.Get("/api/petty-cash/accounts", pettyCashHandler.Accounts)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/petty-cash/opening", pettyCashHandler.RecordOpening)
			r.Post("/api/petty-cash/closing", pettyCashHandler.RecordClosing)
		})

		// Pelacakan Kas — the branch till, reconciled against POS cash takings.
		// Read is open (staff need to see the day before they close it); the
		// counts are admin-only, same rule as the petty cash box.
		r.Get("/api/cash-tracking", cashTrackingHandler.Day)
		r.Get("/api/cash-tracking/history", cashTrackingHandler.History)
		r.Get("/api/cash-tracking/settlement", cashTrackingHandler.Settlement)
		r.Get("/api/cash-tracking/drawer-accounts", cashTrackingHandler.DrawerAccounts)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/cash-tracking/opening", cashTrackingHandler.RecordOpening)
			r.Post("/api/cash-tracking/closing", cashTrackingHandler.RecordClosing)
			r.Put("/api/cash-tracking/drawer-accounts/{id}", cashTrackingHandler.SetDrawerAccount)
		})

		// Setoran — moving cash between the box, the till and the owner's bank.
		// Recording one moves real money, so it is admin-only on both ends.
		r.Get("/api/cash-deposits", cashDepositsHandler.List)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/cash-deposits", cashDepositsHandler.Create)
			r.Post("/api/cash-deposits/{id}/cancel", cashDepositsHandler.Cancel)
		})

		// Dispatches — all authenticated
		r.Get("/api/dispatches", dispatchesHandler.List)
		r.Get("/api/dispatches/{id}", dispatchesHandler.Get)
		r.Post("/api/dispatches", dispatchesHandler.Create)
		r.Put("/api/dispatches/{id}", dispatchesHandler.Update)
		r.Delete("/api/dispatches/{id}", dispatchesHandler.Delete)

		// Dispatch Templates — all authenticated
		r.Get("/api/dispatch-templates", dispatchTemplatesHandler.List)
		r.Get("/api/dispatch-templates/{id}", dispatchTemplatesHandler.Get)
		r.Post("/api/dispatch-templates", dispatchTemplatesHandler.Create)
		r.Put("/api/dispatch-templates/{id}", dispatchTemplatesHandler.Update)
		r.Delete("/api/dispatch-templates/{id}", dispatchTemplatesHandler.Delete)

		// Enumerations — all authenticated
		r.Get("/api/enumerations", enumerationsHandler.List)
		r.Post("/api/enumerations", enumerationsHandler.Create)
		r.Delete("/api/enumerations/{id}", enumerationsHandler.Delete)

		// Recipes — all authenticated
		r.Get("/api/recipes", recipesHandler.List)
		r.Get("/api/recipes/{id}", recipesHandler.Get)
		r.Post("/api/recipes", recipesHandler.Create)
		r.Put("/api/recipes/{id}", recipesHandler.Update)
		r.Delete("/api/recipes/{id}", recipesHandler.Delete)

		// Productions — all authenticated
		r.Get("/api/productions", productionsHandler.List)
		r.Post("/api/productions", productionsHandler.Create)

		// Sales — all authenticated
		r.Get("/api/sales", salesHandler.List)
		r.Post("/api/sales", salesHandler.Create)
		r.Delete("/api/sales/{id}", salesHandler.Delete)

		// POS Import — all authenticated
		r.Post("/api/pos-import/parse", posImportHandler.Parse)
		r.Post("/api/pos-import/confirm", posImportHandler.Confirm)
		r.Get("/api/pos-import", posImportHandler.List)
		r.Delete("/api/pos-import/{id}", posImportHandler.Delete)

		// Activity Log — admin/manager. Staff runs the operation but does not get
		// to read (or prune) the audit trail of who did what.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireReportsAccess)
			r.Get("/api/activity-log", activityLogHandler.List)
			r.Get("/api/activity-log/export", activityLogHandler.Export)
			r.Delete("/api/activity-log", activityLogHandler.DeleteOld)
		})

		// Account Adjustments — all authenticated
		r.Get("/api/account-adjustments", adjustmentsHandler.List)
		r.Post("/api/account-adjustments", adjustmentsHandler.Create)
		r.Post("/api/account-adjustments/transfer", adjustmentsHandler.Transfer)

		// HR Employees & Positions
		// Read (list + detail) allowed for all authenticated users incl. staff.
		r.Get("/api/hr/employees", hrEmployeesHandler.List)
		r.Get("/api/hr/employees/contract-alerts", hrEmployeesHandler.ContractAlerts)
		r.Get("/api/hr/employees/{id}", hrEmployeesHandler.Get)
		r.Get("/api/hr/positions", hrEmployeesHandler.ListPositions)
		// Mutations require HR access (admin, manager, staff, hr).
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Post("/api/hr/employees", hrEmployeesHandler.Create)
			r.Put("/api/hr/employees/{id}", hrEmployeesHandler.Update)
			r.Post("/api/hr/employees/{id}/transition-permanent", hrEmployeesHandler.TransitionToPermanent)
			r.Post("/api/hr/employees/{id}/resign", hrEmployeesHandler.Resign)
			r.Delete("/api/hr/employees/{id}", hrEmployeesHandler.Delete)
			r.Post("/api/hr/employees/{id}/photo", hrEmployeesHandler.UploadPhoto)
			r.Delete("/api/hr/employees/{id}/photo", hrEmployeesHandler.DeletePhoto)
			r.Post("/api/hr/positions", hrEmployeesHandler.CreatePosition)
			r.Put("/api/hr/positions/{id}", hrEmployeesHandler.UpdatePosition)
			r.Delete("/api/hr/positions/{id}", hrEmployeesHandler.DeletePosition)
		})

		// HR Documents — generation (PKWT/PKWTT/Surat Peringatan/Paklaring as
		// DOCX/PDF) and the per-employee signed-document store used by onboarding.
		// Company-level fields (letterhead, signatory, numbering) come from HR
		// settings, not from the request.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Post("/api/hr/documents/generate", hrDocumentsHandler.Generate)
			r.Get("/api/hr/documents/next-number", hrDocumentsHandler.PeekDocumentNumber)
			// Reusable PKWT/PKWTT condition presets, applied to the generator form.
			r.Get("/api/hr/contract-templates", hrDocumentsHandler.ListContractTemplates)
			r.Post("/api/hr/contract-templates", hrDocumentsHandler.CreateContractTemplate)
			r.Put("/api/hr/contract-templates/{id}", hrDocumentsHandler.UpdateContractTemplate)
			r.Delete("/api/hr/contract-templates/{id}", hrDocumentsHandler.DeleteContractTemplate)
			r.Post("/api/hr/documents/next-number", hrDocumentsHandler.ReserveDocumentNumber)
			r.Get("/api/hr/employees/{id}/documents", hrDocumentsHandler.ListEmployeeDocuments)
			r.Post("/api/hr/employees/{id}/documents", hrDocumentsHandler.UploadEmployeeDocument)
			r.Get("/api/hr/employees/{id}/documents/{docId}/download", hrDocumentsHandler.DownloadEmployeeDocument)
			r.Delete("/api/hr/employees/{id}/documents/{docId}", hrDocumentsHandler.DeleteEmployeeDocument)
		})

		// HR Wage module.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			// Wage component catalog
			r.Get("/api/hr/wage-components", hrWagesHandler.ListComponents)
			r.Post("/api/hr/wage-components", hrWagesHandler.CreateComponent)
			r.Put("/api/hr/wage-components/{id}", hrWagesHandler.UpdateComponent)
			r.Delete("/api/hr/wage-components/{id}", hrWagesHandler.DeleteComponent)
			// Per-employee wage structures (versioned)
			r.Get("/api/hr/employees/{id}/wage", hrWagesHandler.GetCurrentWage)
			r.Get("/api/hr/employees/{id}/wage/history", hrWagesHandler.GetWageHistory)
			r.Post("/api/hr/employees/{id}/wage", hrWagesHandler.CreateWageVersion)
		})

		// HR bulk import (employees + initial wage structures).
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Get("/api/hr/import/template", hrImportHandler.Template)
			r.Get("/api/hr/import/export", hrImportHandler.Export)
			r.Post("/api/hr/import/parse", hrImportHandler.Parse)
			r.Post("/api/hr/import/confirm", hrImportHandler.Confirm)
		})

		// HR Attendance — list viewable by all authenticated (including staff).
		r.Get("/api/hr/attendance", attendanceHandler.List)
		// Attendance record entry/correction — admin, manager, and store_manager.
		// store_manager is scoped to attendance only (RequireAttendanceAccess);
		// the configuration/batch endpoints below stay admin/manager.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAttendanceAccess)
			r.Put("/api/hr/attendance/{id}", attendanceHandler.Update)
			// Half-day correction (manual reclassification, no approval).
			r.Post("/api/hr/attendance/{id}/half-day", attendanceHandler.SetHalfDay)
			r.Delete("/api/hr/attendance/{id}/half-day", attendanceHandler.ClearHalfDay)
			// Present-without-scan correction (forgot to check in and out).
			r.Post("/api/hr/attendance/{id}/present-no-punch", attendanceHandler.MarkPresentNoPunch)
			r.Delete("/api/hr/attendance/{id}/present-no-punch", attendanceHandler.ClearPresentNoPunch)
		})
		// Attendance configuration & batch operations require HR access.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Post("/api/hr/attendance/reconcile", attendanceHandler.Reconcile)
			// Face enrollment coverage + device fleet health (web dashboard).
			r.Get("/api/hr/attendance/face/overview", attendanceHandler.FaceOverview)
			// Fingerprint import (two-phase)
			r.Post("/api/hr/attendance/fingerprint-import/parse", attendanceHandler.FingerprintParse)
			r.Post("/api/hr/attendance/fingerprint-import/confirm", attendanceHandler.FingerprintConfirm)
			// Work schedules
			r.Get("/api/hr/attendance/work-schedules", attendanceHandler.ListWorkSchedules)
			r.Post("/api/hr/attendance/work-schedules", attendanceHandler.UpsertWorkSchedule)
			// Public holidays
			r.Get("/api/hr/attendance/holidays", attendanceHandler.ListPublicHolidays)
			r.Post("/api/hr/attendance/holidays", attendanceHandler.CreatePublicHoliday)
			r.Delete("/api/hr/attendance/holidays/{id}", attendanceHandler.DeletePublicHoliday)
			// Attendance devices
			// Self-service enrollment: Android app posts its JWT + device_name,
			// server mints and returns a device_key. Replaces paste-from-web.
			r.Post("/api/hr/attendance/device/enroll", attendanceDeviceHandler.Enroll)
			r.Get("/api/hr/attendance/devices", attendanceHandler.ListDevices)
			r.Post("/api/hr/attendance/devices", attendanceHandler.CreateDevice)
			r.Put("/api/hr/attendance/devices/{id}", attendanceHandler.SetDeviceActive)
			r.Delete("/api/hr/attendance/devices/{id}", attendanceHandler.DeleteDevice)
		})

		// HR Performance scoring.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			// Policies (CRUD; delete deactivates when referenced)
			r.Get("/api/hr/performance/policies", performanceHandler.ListPolicies)
			r.Post("/api/hr/performance/policies", performanceHandler.CreatePolicy)
			r.Put("/api/hr/performance/policies/{id}", performanceHandler.UpdatePolicy)
			r.Delete("/api/hr/performance/policies/{id}", performanceHandler.DeletePolicy)
			// Policies Excel import/export (distinct paths so they don't collide with
			// the /policies/{id} param route in chi's radix tree).
			r.Get("/api/hr/performance/policies-export", performanceHandler.ExportPolicies)
			r.Post("/api/hr/performance/policies-import", performanceHandler.ImportPolicies)
			// Scores & breakdown
			r.Get("/api/hr/performance/scores", performanceHandler.ListScores)
			r.Get("/api/hr/employees/{id}/performance", performanceHandler.EmployeePerformance)
			// Violations
			r.Post("/api/hr/performance/violations", performanceHandler.CreateManualViolation)
			r.Delete("/api/hr/performance/violations/auto", performanceHandler.ResetAutoViolations)
			r.Delete("/api/hr/performance/violations/{id}", performanceHandler.DeleteViolation)
			// Manual backfill
			r.Post("/api/hr/performance/evaluate", performanceHandler.Evaluate)
		})

		// HR Leave management.
		// Manpower planning viewable by all authenticated (including staff).
		r.Get("/api/hr/manpower-planning", leaveHandler.GetManpowerPlanning)
		// Most endpoints need HR access; approval/rejection are manager-only.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			// Leave types (CRUD; delete deactivates when referenced)
			r.Get("/api/hr/leave-types", leaveHandler.ListLeaveTypes)
			r.Post("/api/hr/leave-types", leaveHandler.CreateLeaveType)
			r.Put("/api/hr/leave-types/{id}", leaveHandler.UpdateLeaveType)
			r.Delete("/api/hr/leave-types/{id}", leaveHandler.DeleteLeaveType)
			// Leave requests
			r.Get("/api/hr/leave-requests", leaveHandler.ListLeaveRequests)
			r.Post("/api/hr/leave-requests", leaveHandler.CreateLeaveRequest)
			r.Post("/api/hr/leave-requests/{id}/cancel", leaveHandler.CancelLeaveRequest)
			// Balances + per-employee history
			r.Get("/api/hr/employees/{id}/leave-balance", leaveHandler.GetLeaveBalance)
			r.Put("/api/hr/employees/{id}/leave-balance", leaveHandler.SetLeaveBalanceQuota)
			r.Get("/api/hr/employees/{id}/leave-requests", leaveHandler.ListEmployeeLeaveRequests)
		})
		// HR Leave approval / rejection — manager only.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireManager)
			r.Post("/api/hr/leave-requests/{id}/approve", leaveHandler.ApproveLeaveRequest)
			r.Post("/api/hr/leave-requests/{id}/reject", leaveHandler.RejectLeaveRequest)
			r.Post("/api/hr/leave-requests/bulk-approve", leaveHandler.BulkApproveLeaveRequests)
			r.Post("/api/hr/leave-requests/bulk-reject", leaveHandler.BulkRejectLeaveRequests)
		})

		// HR Kasbon (cash advance) — list and detail viewable by all authenticated.
		r.Get("/api/hr/kasbons", kasbonHandler.List)
		r.Get("/api/hr/kasbons/{id}", kasbonHandler.Get)
		// Mutations require HR access.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Post("/api/hr/kasbons", kasbonHandler.Create)
			r.Put("/api/hr/kasbons/{id}", kasbonHandler.Update)
			r.Post("/api/hr/kasbons/{id}/process", kasbonHandler.Process)
			r.Post("/api/hr/kasbons/{id}/cancel", kasbonHandler.Cancel)
		})
		// HR Kasbon approval / rejection — manager only.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireManager)
			r.Post("/api/hr/kasbons/{id}/approve", kasbonHandler.Approve)
			r.Post("/api/hr/kasbons/{id}/reject", kasbonHandler.Reject)
		})

		// HR Overtime requests — read/create/cancel: HR access.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Get("/api/hr/overtime", overtimeHandler.List)
			r.Post("/api/hr/overtime", overtimeHandler.Create)
			r.Post("/api/hr/overtime/{id}/cancel", overtimeHandler.Cancel)
			r.Delete("/api/hr/overtime/{id}", overtimeHandler.Delete)
		})
		// HR Overtime approval / rejection — manager only.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireManager)
			r.Post("/api/hr/overtime/{id}/approve", overtimeHandler.Approve)
			r.Post("/api/hr/overtime/{id}/reject", overtimeHandler.Reject)
		})

		// HR Payroll (penggajian).
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Get("/api/hr/payroll/periods", payrollHandler.ListPeriods)
			r.Post("/api/hr/payroll/periods", payrollHandler.CreatePeriod)
			r.Get("/api/hr/payroll/periods/{id}", payrollHandler.GetPeriod)
			r.Get("/api/hr/payroll/periods/{id}/lines", payrollHandler.ListLines)
			r.Get("/api/hr/payroll/periods/{id}/bonus-eligible", payrollHandler.BonusEligible)
			r.Post("/api/hr/payroll/periods/{id}/apply-bonus", payrollHandler.ApplyBonus)
			r.Post("/api/hr/payroll/periods/{id}/regenerate-line/{employeeId}", payrollHandler.RegenerateLine)
			r.Delete("/api/hr/payroll/periods/{id}", payrollHandler.DeletePeriod)
			r.Post("/api/hr/payroll/periods/{id}/close", payrollHandler.ClosePeriod)
			r.Post("/api/hr/payroll/periods/{id}/mark-paid", payrollHandler.MarkPaid)
			r.Post("/api/hr/payroll/periods/{id}/post-accounting", payrollHandler.RetryPosting)
			r.Post("/api/hr/payroll/periods/{id}/review-all", payrollHandler.ReviewAll)
			r.Get("/api/hr/payroll/lines/{id}/review", payrollHandler.GetLineReview)
			r.Post("/api/hr/payroll/lines/{id}/review", payrollHandler.ReviewLine)
			r.Post("/api/hr/payroll/lines/{id}/unreview", payrollHandler.UnreviewLine)

			// Payslips (slip gaji) — PDF single + ZIP batch. Reject open periods (409).
			r.Get("/api/hr/payroll/lines/{id}/payslip", payslipHandler.DownloadPayslip)
			r.Get("/api/hr/payroll/periods/{id}/payslips", payslipHandler.DownloadPeriodPayslips)

			// THR (Tunjangan Hari Raya) — runs mirror payroll periods.
			r.Get("/api/hr/thr/runs", thrHandler.ListRuns)
			r.Post("/api/hr/thr/runs", thrHandler.CreateRun)
			r.Get("/api/hr/thr/runs/{id}", thrHandler.GetRun)
			r.Get("/api/hr/thr/runs/{id}/lines", thrHandler.ListLines)
			r.Delete("/api/hr/thr/runs/{id}", thrHandler.DeleteRun)
			r.Post("/api/hr/thr/runs/{id}/close", thrHandler.CloseRun)
			r.Post("/api/hr/thr/runs/{id}/mark-paid", thrHandler.MarkPaid)
			r.Post("/api/hr/thr/runs/{id}/review-all", thrHandler.ReviewAll)
			r.Post("/api/hr/thr/runs/{id}/regenerate-line/{employeeId}", thrHandler.RegenerateLine)
			r.Get("/api/hr/thr/lines/{id}/review", thrHandler.GetLineReview)
			r.Post("/api/hr/thr/lines/{id}/review", thrHandler.ReviewLine)
			r.Post("/api/hr/thr/lines/{id}/unreview", thrHandler.UnreviewLine)
			r.Get("/api/hr/thr/lines/{id}/payslip", thrHandler.DownloadPayslip)
			r.Get("/api/hr/thr/runs/{id}/payslips", thrHandler.DownloadRunPayslips)

			// HR settings — read needs HR access (header info needed to preview).
			r.Get("/api/hr/settings", payslipHandler.GetSettings)

			// Staff KPIs — measured against the daily task board, managed from
			// HR settings because they are a people-performance instrument.
			r.Get("/api/hr/kpi", tasksHandler.ListKPIs)
			r.Post("/api/hr/kpi", tasksHandler.CreateKPI)
			r.Put("/api/hr/kpi/{id}", tasksHandler.UpdateKPI)
			r.Delete("/api/hr/kpi/{id}", tasksHandler.DeleteKPI)
			r.Get("/api/hr/kpi/scores", tasksHandler.KPIScores)
		})

		// HR settings mutations — HR access. This is where the company data that
		// feeds payslips and generated documents lives, so whoever runs HR needs
		// to be able to change it.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireHRAccess)
			r.Put("/api/hr/settings", payslipHandler.UpdateSettings)
			r.Post("/api/hr/settings/logo", payslipHandler.UploadLogo)
		})

		// Reports — admin/manager only. This is the one part of the operational
		// app staff does not get: the org-wide financial picture.
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireReportsAccess)
			r.Get("/api/reports/financial", reportsHandler.Financial)
			r.Get("/api/reports/profit-loss-by-branch", reportsHandler.ProfitLossByBranch)
			r.Get("/api/reports/profit-loss-periodic", reportsHandler.ProfitLossPeriodic)
			r.Get("/api/reports/cash-summary", reportsHandler.CashSummary)
			r.Get("/api/reports/daily", reportsHandler.Daily)
			r.Get("/api/reports/inventory-value", reportsHandler.InventoryValue)
			r.Get("/api/reports/expense-summary", reportsHandler.ExpenseSummary)
			r.Get("/api/expense-report", reportsHandler.ExpenseReport)
			r.Get("/api/reports/price-changes", analyticsHandler.PriceChanges)
			r.Get("/api/reports/usage-trend", analyticsHandler.UsageTrend)
		})

		// Daily tasks & notifications.
		//
		// The bell feed is per-user by role and always available; the task board
		// and its definitions are the operational desk's, so the HR-only role is
		// already excluded from them by RestrictHRRole.
		r.Get("/api/notifications", tasksHandler.Notifications)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireCoreAccess)
			r.Get("/api/tasks/daily", tasksHandler.Board)
			r.Get("/api/tasks/pending", tasksHandler.Pending)
			r.Post("/api/tasks/daily/complete", tasksHandler.CompleteTask)
			r.Get("/api/tasks/definitions", tasksHandler.ListDefinitions)
			r.Post("/api/tasks/definitions", tasksHandler.CreateDefinition)
			r.Put("/api/tasks/definitions/{id}", tasksHandler.UpdateDefinition)
			r.Delete("/api/tasks/definitions/{id}", tasksHandler.DeleteDefinition)
		})

		// Stats — all authenticated. These back the dashboard tiles and charts,
		// not the reporting screens, so staff keeps them.
		r.Get("/api/stats", statsHandler.GeneralStats)
		r.Get("/api/stats/daily-sales", statsHandler.DailySales)
		r.Get("/api/stats/stock-flow", statsHandler.StockFlow)
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		log.Printf("Starting server on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down server...")

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutCtx); err != nil {
		log.Printf("server forced to shutdown: %v", err)
	}

	// Fresh context, not ctx: the signal context is already cancelled by now, so
	// reusing it would abort this write immediately. Written after Shutdown
	// returns so the recorded uptime covers the whole serving period, and while
	// the pool is still open (it is closed by the deferred pool.Close above).
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := service.LogSystemEvent(stopCtx, queries, service.ActionServerStop,
		fmt.Sprintf("Server dihentikan setelah berjalan %s", formatUptime(time.Since(startedAt)))); err != nil {
		log.Printf("activity log: failed to record server stop: %v", err)
	}
	stopCancel()

	log.Println("server stopped")
}
