package db

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // postgres driver
	_ "github.com/golang-migrate/migrate/v4/source/file"       // file source
)

// Migrate applies all pending up migrations from the given migrations directory
// against the target database URL.
func Migrate(databaseURL, migrationsDir string) error {
	sourceURL := "file://" + migrationsDir

	m, err := migrate.New(sourceURL, databaseURL)
	if err != nil {
		return fmt.Errorf("db: create migrator: %w", err)
	}

	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			slog.Error("migration source close error", "layer", "db", "error", srcErr)
		}

		if dbErr != nil {
			slog.Error("migration db close error", "layer", "db", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: apply migrations: %w", err)
	}

	slog.Info("migrations applied", "layer", "db", "source", migrationsDir)

	return nil
}
