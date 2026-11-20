// reset-password sets a user's password from the command line, and with
// -create-admin also bootstraps the first admin account on a fresh database
// (no migration creates a user).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
)

func main() {
	createAdmin := flag.Bool("create-admin", false, "create the user with role admin if it does not exist")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: reset-password [-create-admin] <username> <new-password>\n")
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}
	// Login lowercases the username before looking it up, so store it that way.
	username := strings.ToLower(strings.TrimSpace(flag.Arg(0)))
	password := flag.Arg(1)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("failed to hash password: %v", err)
	}

	tag, err := pool.Exec(ctx,
		"UPDATE users SET password_hash = $1 WHERE username = $2",
		string(hash), username,
	)
	if err != nil {
		log.Fatalf("failed to update password: %v", err)
	}
	if tag.RowsAffected() > 0 {
		fmt.Printf("Password for user %q updated successfully.\n", username)
		return
	}
	if !*createAdmin {
		log.Fatalf("user %q not found (pass -create-admin to create it)", username)
	}

	if _, err := pool.Exec(ctx,
		"INSERT INTO users (username, password_hash, role) VALUES ($1, $2, 'admin')",
		username, string(hash),
	); err != nil {
		log.Fatalf("failed to create user: %v", err)
	}
	fmt.Printf("Admin user %q created.\n", username)
}
