package jobstore

import (
	"context"
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migrate runs all pending SQL migrations from migrationsFS against db.
// Pass an fs.FS rooted at the directory containing the *.sql files.
func Migrate(ctx context.Context, db *sql.DB, migrationsFS fs.FS) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrationsFS)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}
