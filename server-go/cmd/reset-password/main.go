package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"

	"inventory-app/server-go/internal/config"
	"inventory-app/server-go/internal/db"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: reset-password <username> <new-password>\n")
		os.Exit(1)
	}
	username := os.Args[1]
	password := os.Args[2]

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
	if tag.RowsAffected() == 0 {
		log.Fatalf("user %q not found", username)
	}

	fmt.Printf("Password for user %q updated successfully.\n", username)
}
