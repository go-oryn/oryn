package sql_test

import (
	"context"
	stdsql "database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/health"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/sql"
)

// sqliteValues returns the config of a SQLite database in a temporary file.
func sqliteValues(t *testing.T) map[string]any {
	t.Helper()

	return map[string]any{
		"sql.driver": "sqlite",
		"sql.dsn":    filepath.Join(t.TempDir(), "test.db"),
	}
}

// unreachableValues returns the config of a database nobody listens to.
func unreachableValues(driver string) map[string]any {
	dsn := map[string]string{
		"postgres": "postgres://test_user@127.0.0.1:1/test?connect_timeout=1",
		"mysql":    "test_user@tcp(127.0.0.1:1)/test?timeout=1s",
	}[driver]

	return map[string]any{"sql.driver": driver, "sql.dsn": dsn}
}

// newApp returns an application with the sql module and the config values.
func newApp(values map[string]any, options ...fx.Option) core.App {
	return core.New(append(options, sql.Module, config.AsOptions(config.WithValues(values)))...)
}

// startError returns the error of an application failing to build or start.
func startError(t *testing.T, app core.App, options ...fx.Option) error {
	t.Helper()

	fxApp := fx.New(app.Options(append(options, coretest.TestOptions(), fx.NopLogger, fx.Invoke(func(*stdsql.DB) {}))...)...)
	if err := fxApp.Err(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := fxApp.Start(ctx)
	if err == nil {
		require.NoError(t, fxApp.Stop(ctx))
	}

	return err
}

func TestNewDB(t *testing.T) {
	t.Parallel()

	t.Run("queries are instrumented", func(t *testing.T) {
		t.Parallel()

		var (
			db      *stdsql.DB
			spans   *tracetest.InMemoryExporter
			metrics *sdkmetric.ManualReader
		)

		coretest.Start(t, newApp(sqliteValues(t)), fx.Populate(&db, &spans, &metrics))

		var result int
		require.NoError(t, db.QueryRowContext(t.Context(), "select 1 + 1").Scan(&result))
		assert.Equal(t, 2, result)

		_, err := db.ExecContext(t.Context(), "CREATE TABLE test_table (id INTEGER)")
		require.NoError(t, err)

		spantest.HasSpan(t, spans,
			spantest.WithName("SELECT"),
			spantest.WithAttributeValue(semconv.DBSystemNameSQLite),
			spantest.WithAttributeValue(semconv.DBQueryText("select 1 + 1")),
		)
		spantest.HasSpan(t, spans, spantest.WithName("CREATE"))
		metrictest.HasMetric(t, metrics, metrictest.WithName("db.client.operation.duration"))
		metrictest.HasMetric(t, metrics, metrictest.WithName("db.sql.connection.open"))
	})

	t.Run("calls without query", func(t *testing.T) {
		t.Parallel()

		var (
			db    *stdsql.DB
			spans *tracetest.InMemoryExporter
		)

		coretest.Start(t, newApp(sqliteValues(t)), fx.Populate(&db, &spans))

		tx, err := db.BeginTx(t.Context(), nil)
		require.NoError(t, err)
		require.NoError(t, tx.Rollback())

		spantest.HasSpan(t, spans, spantest.WithName("sql.conn.begin_tx"))
	})

	t.Run("query text not recorded", func(t *testing.T) {
		t.Parallel()

		var (
			db    *stdsql.DB
			spans *tracetest.InMemoryExporter
		)

		values := sqliteValues(t)
		values["sql.observability.query"] = false

		coretest.Start(t, newApp(values), fx.Populate(&db, &spans))

		_, err := db.ExecContext(t.Context(), "select 1")
		require.NoError(t, err)

		spantest.HasSpan(t, spans, spantest.WithName("SELECT"))
		spantest.HasNoSpan(t, spans, spantest.WithAttribute(semconv.DBQueryTextKey))
	})

	t.Run("pool settings", func(t *testing.T) {
		t.Parallel()

		var db *stdsql.DB

		values := sqliteValues(t)
		values["sql.max_open_conns"] = 3

		coretest.Start(t, newApp(values), fx.Populate(&db))

		assert.Equal(t, 3, db.Stats().MaxOpenConnections)
	})

	t.Run("closed when the application stops", func(t *testing.T) {
		t.Parallel()

		var db *stdsql.DB

		app := coretest.New(t, newApp(sqliteValues(t)), fx.Populate(&db))
		app.RequireStart().RequireStop()

		require.ErrorContains(t, db.PingContext(t.Context()), "database is closed")
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			values map[string]any
			err    string
		}{
			"missing driver":    {values: map[string]any{"sql.dsn": "test"}, err: `unsupported sql.driver ""`},
			"unknown driver":    {values: map[string]any{"sql.driver": "oracle", "sql.dsn": "test"}, err: `unsupported sql.driver "oracle"`},
			"missing dsn":       {values: map[string]any{"sql.driver": "sqlite"}, err: "missing sql.dsn"},
			"invalid dsn":       {values: map[string]any{"sql.driver": "mysql", "sql.dsn": "invalid"}, err: "cannot open mysql database"},
			"unreachable mysql": {values: unreachableValues("mysql"), err: "cannot connect to the mysql database"},
			"unreachable pg":    {values: unreachableValues("postgres"), err: "cannot connect to the postgres database"},
		} {
			require.ErrorContains(t, startError(t, newApp(tc.values)), tc.err, name)
		}
	})

	t.Run("no ping on start", func(t *testing.T) {
		t.Parallel()

		values := unreachableValues("postgres")
		values["sql.ping_on_start"] = false

		var checker *health.Checker

		coretest.Start(t, newApp(values), fx.Populate(&checker))

		result := checker.Check(t.Context(), health.Readiness)
		assert.False(t, result.Healthy())
		assert.Equal(t, health.Unhealthy, result.Probes["sql"].Status)
	})
}

func TestProbe(t *testing.T) {
	t.Parallel()

	var checker *health.Checker

	coretest.Start(t, newApp(sqliteValues(t)), fx.Populate(&checker))

	for _, kind := range []health.Kind{health.Readiness, health.Startup} {
		result := checker.Check(t.Context(), kind)
		assert.True(t, result.Healthy(), kind)
		assert.Equal(t, health.Healthy, result.Probes["sql"].Status, kind)
	}

	assert.NotContains(t, checker.Check(t.Context(), health.Liveness).Probes, "sql", "a database outage must not restart the application")
}

func TestDriverOf(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"postgres", "mysql", "sqlite", " SQLite "} {
		driver, err := sql.DriverOf(name)
		require.NoError(t, err, name)
		assert.NotEmpty(t, driver.Name, name)
	}
}
