package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
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
)

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

	authHandler := handler.NewAuthHandler(queries, cfg.JWTSecret)
	usersHandler := handler.NewUsersHandler(queries)
	warehousesHandler := handler.NewWarehousesHandler(pool, queries)
	vendorsHandler := handler.NewVendorsHandler(queries)
	itemsHandler := handler.NewItemsHandler(queries)
	accountsHandler := handler.NewAccountsHandler(queries)
	templatesHandler := handler.NewInvoiceTemplatesHandler(pool, queries)
	branchesHandler := handler.NewBranchesHandler(pool, queries)
	divisionsHandler := handler.NewDivisionsHandler(pool, queries)
	inventoryHandler := handler.NewInventoryHandler(pool, queries)
	stockHistoryHandler := handler.NewStockHistoryHandler(queries)
	stockTransfersHandler := handler.NewStockTransfersHandler(pool, queries)
	stockOpnameHandler := handler.NewStockOpnameHandler(pool, queries)
	invoicesHandler := handler.NewInvoicesHandler(pool, queries)
	invoicesHandler.SetUploadsDir(cfg.UploadsDir)
	dispatchesHandler := handler.NewDispatchesHandler(pool, queries)
	enumerationsHandler := handler.NewEnumerationsHandler(pool, queries)
	recipesHandler := handler.NewRecipesHandler(pool, queries)
	productionsHandler := handler.NewProductionsHandler(pool, queries)
	salesHandler := handler.NewSalesHandler(pool, queries)
	posImportHandler := handler.NewPOSImportHandler(pool, queries)
	activityLogHandler := handler.NewActivityLogHandler(queries)
	adjustmentsHandler := handler.NewAccountAdjustmentsHandler(pool, queries)
	reportsHandler := handler.NewReportsHandler(pool, queries)
	statsHandler := handler.NewStatsHandler(pool)

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

	// Public auth routes
	r.With(appmiddleware.LoginRateLimit()).Post("/api/auth/login", authHandler.Login)
	r.Post("/api/auth/refresh", authHandler.Refresh)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(appmiddleware.Authenticate(queries, cfg.JWTSecret))
		r.Post("/api/auth/logout", authHandler.Logout)

		// Users — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/users", usersHandler.List)
			r.Post("/api/users", usersHandler.Create)
			r.Put("/api/users/{id}", usersHandler.Update)
			r.Delete("/api/users/{id}", usersHandler.Delete)
		})

		// Warehouses
		r.Get("/api/warehouses", warehousesHandler.List)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/warehouses", warehousesHandler.Create)
			r.Put("/api/warehouses/{id}", warehousesHandler.Update)
			r.Delete("/api/warehouses/{id}", warehousesHandler.Delete)
		})

		// Vendors
		r.Get("/api/vendors", vendorsHandler.List)
		r.Get("/api/vendors/{id}/history", vendorsHandler.GetHistory)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/vendors", vendorsHandler.Create)
			r.Put("/api/vendors/{id}", vendorsHandler.Update)
			r.Delete("/api/vendors/{id}", vendorsHandler.Delete)
		})

		// Items
		r.Get("/api/items", itemsHandler.List)
		r.Get("/api/items/{id}", itemsHandler.Get)
		r.Get("/api/items/{id}/last-price", itemsHandler.GetLastPrice)
		r.Get("/api/items/{id}/history", itemsHandler.GetHistory)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/items", itemsHandler.Create)
			r.Put("/api/items/{id}", itemsHandler.Update)
			r.Delete("/api/items/{id}", itemsHandler.Delete)
		})

		// Accounts — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/accounts", accountsHandler.List)
			r.Post("/api/accounts", accountsHandler.Create)
			r.Put("/api/accounts/{id}", accountsHandler.Update)
			r.Delete("/api/accounts/{id}", accountsHandler.Delete)
		})

		// Invoice Templates — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/invoice-templates", templatesHandler.List)
			r.Get("/api/invoice-templates/{id}", templatesHandler.Get)
			r.Post("/api/invoice-templates", templatesHandler.Create)
			r.Put("/api/invoice-templates/{id}", templatesHandler.Update)
			r.Delete("/api/invoice-templates/{id}", templatesHandler.Delete)
		})

		// Branches — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/branches", branchesHandler.List)
			r.Get("/api/branches/{id}", branchesHandler.Get)
			r.Post("/api/branches", branchesHandler.Create)
			r.Put("/api/branches/{id}", branchesHandler.Update)
			r.Delete("/api/branches/{id}", branchesHandler.Delete)
		})

		// Divisions — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/divisions", divisionsHandler.List)
			r.Post("/api/divisions", divisionsHandler.Create)
			r.Put("/api/divisions/{id}", divisionsHandler.Update)
			r.Delete("/api/divisions/{id}", divisionsHandler.Delete)
			r.Get("/api/division-categories", divisionsHandler.ListCategories)
			r.Post("/api/division-categories", divisionsHandler.CreateCategory)
			r.Delete("/api/division-categories/{id}", divisionsHandler.DeleteCategory)
		})

		// Inventory — all authenticated
		r.Get("/api/inventory", inventoryHandler.List)
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

		// Stock Opname — all authenticated
		r.Get("/api/stock-opname", stockOpnameHandler.List)
		r.Get("/api/stock-opname/{id}", stockOpnameHandler.Get)
		r.Post("/api/stock-opname", stockOpnameHandler.Create)

		// Invoices — all authenticated; delete admin only
		r.Get("/api/invoices", invoicesHandler.List)
		r.Get("/api/invoices/{id}", invoicesHandler.Get)
		r.Post("/api/invoices", invoicesHandler.Create)
		r.Put("/api/invoices/{id}", invoicesHandler.Update)
		r.Post("/api/invoices/{id}/pay", invoicesHandler.Pay)
		r.Post("/api/invoices/{id}/photo", invoicesHandler.UploadPhoto)
		r.Delete("/api/invoices/{id}/photo", invoicesHandler.DeletePhoto)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Delete("/api/invoices/{id}", invoicesHandler.Delete)
		})

		// Dispatches — all authenticated
		r.Get("/api/dispatches", dispatchesHandler.List)
		r.Get("/api/dispatches/{id}", dispatchesHandler.Get)
		r.Post("/api/dispatches", dispatchesHandler.Create)

		// Enumerations — all authenticated; delete admin only
		r.Get("/api/enumerations", enumerationsHandler.List)
		r.Post("/api/enumerations", enumerationsHandler.Create)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Delete("/api/enumerations/{id}", enumerationsHandler.Delete)
		})

		// Recipes — list/get all authenticated; create/update/delete admin only
		r.Get("/api/recipes", recipesHandler.List)
		r.Get("/api/recipes/{id}", recipesHandler.Get)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Post("/api/recipes", recipesHandler.Create)
			r.Put("/api/recipes/{id}", recipesHandler.Update)
			r.Delete("/api/recipes/{id}", recipesHandler.Delete)
		})

		// Productions — all authenticated
		r.Get("/api/productions", productionsHandler.List)
		r.Post("/api/productions", productionsHandler.Create)

		// Sales — list/create all authenticated; delete admin only
		r.Get("/api/sales", salesHandler.List)
		r.Post("/api/sales", salesHandler.Create)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Delete("/api/sales/{id}", salesHandler.Delete)
		})

		// POS Import — parse/confirm/list all authenticated; delete admin only
		r.Post("/api/pos-import/parse", posImportHandler.Parse)
		r.Post("/api/pos-import/confirm", posImportHandler.Confirm)
		r.Get("/api/pos-import", posImportHandler.List)
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Delete("/api/pos-import/{id}", posImportHandler.Delete)
		})

		// Activity Log & Account Adjustments — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/activity-log", activityLogHandler.List)
			r.Get("/api/activity-log/export", activityLogHandler.Export)
			r.Delete("/api/activity-log", activityLogHandler.DeleteOld)
			r.Get("/api/account-adjustments", adjustmentsHandler.List)
			r.Post("/api/account-adjustments", adjustmentsHandler.Create)
			r.Post("/api/account-adjustments/transfer", adjustmentsHandler.Transfer)
		})

		// Reports & Stats — all admin
		r.Group(func(r chi.Router) {
			r.Use(appmiddleware.RequireAdmin)
			r.Get("/api/reports/financial", reportsHandler.Financial)
			r.Get("/api/reports/daily", reportsHandler.Daily)
			r.Get("/api/reports/inventory-value", reportsHandler.InventoryValue)
			r.Get("/api/reports/expense-summary", reportsHandler.ExpenseSummary)
			r.Get("/api/expense-report", reportsHandler.ExpenseReport)
			r.Get("/api/stats", statsHandler.GeneralStats)
			r.Get("/api/stats/daily-sales", statsHandler.DailySales)
			r.Get("/api/stats/stock-flow", statsHandler.StockFlow)
		})
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
	log.Println("server stopped")
}
