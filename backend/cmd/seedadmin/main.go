// Command seedadmin creates the first admin account:
//
//	go run ./backend/cmd/seedadmin -db data/app.db <username>
//
// The password is read from stdin (visible prompt; run locally, once).
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"web-go/backend/internal/auth"
	"web-go/backend/internal/database"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seedadmin:", err)
		os.Exit(1)
	}
}

func run() error {
	dbPath := flag.String("db", "data/app.db", "sqlite path")
	flag.Parse()
	if flag.NArg() != 1 {
		return fmt.Errorf("usage: seedadmin [-db path] <username>")
	}
	username := strings.TrimSpace(flag.Arg(0))
	if username == "" {
		return fmt.Errorf("empty username")
	}
	fmt.Print("Password: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return err
	}
	password := strings.TrimSpace(line)
	if password == "" {
		return fmt.Errorf("empty password")
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	db, err := database.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, db); err != nil {
		return err
	}
	id, err := database.CreateUser(ctx, db, username, hash, "admin")
	if err != nil {
		if errors.Is(err, database.ErrAlreadyExists) {
			return fmt.Errorf("user %q already exists", username)
		}
		return err
	}
	fmt.Printf("created admin %q (id %d)\n", username, id)
	return nil
}
