package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

const migrationsGroup = `group:"oryn-sql-migrations"`

// AsMigrations registers the migrations: the goose SQL files at the root of fsys
// (e.g. 00001_create_users.sql), applied by [Migrate]. Register them once.
//
//	-- +goose Up
//	CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
//
//	-- +goose Down
//	DROP TABLE users;
//
// See https://pressly.github.io/goose/documentation/annotations/.
func AsMigrations(fsys fs.FS) fx.Option {
	return fx.Supply(fx.Annotate(fsys, fx.As(new(fs.FS)), fx.ResultTags(migrationsGroup)))
}

// MigrateParams are the dependencies of [Migrate].
type MigrateParams struct {
	fx.In

	Lifecycle      fx.Lifecycle
	Driver         Driver
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	DB             *stdsql.DB
	Migrations     []fs.FS `group:"oryn-sql-migrations"`
}

// Migrate applies the pending migrations registered with [AsMigrations] when the application
// starts, before the start hooks registered after it: pass it before the servers to Run.
// The application fails to start if a migration fails.
func Migrate() fx.Option {
	return fx.Invoke(func(params MigrateParams) error {
		switch len(params.Migrations) {
		case 0:
			return errors.New("no migrations registered with sql.AsMigrations")
		case 1:
		default:
			return fmt.Errorf("%d migration file systems registered with sql.AsMigrations, expected 1", len(params.Migrations))
		}

		provider, err := goose.NewProvider(goose.Dialect(params.Driver.dialect), params.DB, params.Migrations[0],
			goose.WithSlog(params.Logger),
		)
		if err != nil {
			return fmt.Errorf("cannot load the migrations: %w", err)
		}

		tracer := params.TracerProvider.Tracer("github.com/go-oryn/oryn/sql")

		params.Lifecycle.Append(fx.StartHook(func(ctx context.Context) error {
			// One trace for the whole migration, instead of one per query.
			ctx, span := tracer.Start(ctx, "sql.migrate")
			defer span.End()

			results, err := provider.Up(ctx)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())

				return fmt.Errorf("cannot apply the migrations: %w", err)
			}

			span.SetAttributes(attribute.Int("sql.migrations.applied", len(results)))

			params.Logger.InfoContext(ctx, "database migrated", "applied", len(results))

			return nil
		}))

		return nil
	})
}
