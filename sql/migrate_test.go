package sql_test

import (
	stdsql "database/sql"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/sql"
)

func newMigrations() fstest.MapFS {
	return fstest.MapFS{
		"00001_create_users.sql": {Data: []byte(`
-- +goose Up
CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);

-- +goose Down
DROP TABLE users;
`)},
		"00002_insert_user.sql": {Data: []byte(`
-- +goose Up
INSERT INTO users (name) VALUES ('test_user');
`)},
	}
}

func TestMigrate(t *testing.T) {
	t.Parallel()

	t.Run("applies the migrations on start", func(t *testing.T) {
		t.Parallel()

		var (
			db   *stdsql.DB
			logs *logtest.InMemoryExporter
		)

		coretest.Start(t, newApp(sqliteValues(t), sql.AsMigrations(newMigrations())), sql.Migrate(), fx.Populate(&db, &logs))

		var name string
		require.NoError(t, db.QueryRowContext(t.Context(), "SELECT name FROM users").Scan(&name))
		assert.Equal(t, "test_user", name)

		logtest.HasRecord(t, logs, logtest.WithBody("database migrated"), logtest.WithAttributeValue("applied", "2"))
	})

	t.Run("applies only the pending migrations", func(t *testing.T) {
		t.Parallel()

		values := sqliteValues(t)

		for _, applied := range []string{"2", "0"} {
			var logs *logtest.InMemoryExporter

			app := coretest.New(t, newApp(values, sql.AsMigrations(newMigrations())), sql.Migrate(), fx.Populate(&logs))
			app.RequireStart().RequireStop()

			logtest.HasRecord(t, logs, logtest.WithBody("database migrated"), logtest.WithAttributeValue("applied", applied))
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			options []fx.Option
			err     string
		}{
			"no migrations": {
				err: "no migrations registered with sql.AsMigrations",
			},
			"several migrations": {
				options: []fx.Option{sql.AsMigrations(newMigrations()), sql.AsMigrations(newMigrations())},
				err:     "2 migration file systems registered with sql.AsMigrations, expected 1",
			},
			"no migration files": {
				options: []fx.Option{sql.AsMigrations(fstest.MapFS{})},
				err:     "cannot load the migrations",
			},
			"failing migration": {
				options: []fx.Option{sql.AsMigrations(fstest.MapFS{
					"00001_invalid.sql": {Data: []byte("-- +goose Up\nCREATE TABLE (;\n")},
				})},
				err: "cannot apply the migrations",
			},
		} {
			err := startError(t, newApp(sqliteValues(t), tc.options...), sql.Migrate())
			require.ErrorContains(t, err, tc.err, name)
		}
	})
}
