// Package sql provides a [database/sql] connection pool to PostgreSQL, MySQL or SQLite,
// instrumented with OpenTelemetry, with a health probe and migrations.
//
//	core.New(
//		core.WithConfigFS(configs.FS),
//		sql.Module,
//		sql.AsMigrations(migrations.FS),
//	).Run(sql.Migrate(), httpserver.Serve())
//
// The pool is provided as a [*database/sql.DB].
package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/XSAM/otelsql"
	// Database drivers.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	_ "modernc.org/sqlite"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/health"
)

// ModuleName is the name of the sql fx module.
const ModuleName = "sql"

// Module provides the [*database/sql.DB] and its [Driver], and registers its readiness and startup [health.Probe].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewDriver, NewDB),
	health.AsProbe(NewProbe, health.Readiness, health.Startup),
)

// NewDBParams are the dependencies of [NewDB].
type NewDBParams struct {
	fx.In

	Lifecycle      fx.Lifecycle
	Config         *config.Config
	Driver         Driver
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// NewDB opens the connection pool configured by the sql.* keys, instrumented with OpenTelemetry.
// Unless sql.ping_on_start is false, the application fails to start if the database is unreachable.
// The pool is closed when the application stops.
func NewDB(params NewDBParams) (*stdsql.DB, error) {
	cfg := params.Config
	driver := params.Driver

	dsn := cfg.GetString("sql.dsn")
	if dsn == "" {
		return nil, errors.New("missing sql.dsn")
	}

	options := []otelsql.Option{
		otelsql.WithTracerProvider(params.TracerProvider),
		otelsql.WithMeterProvider(params.MeterProvider),
		otelsql.WithTextMapPropagator(params.Propagator),
		otelsql.WithAttributes(driver.system),
		otelsql.WithSpanNameFormatter(spanName),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			DisableErrSkip:       true,
			DisableQuery:         !cfg.GetBoolOrDefault("sql.observability.query", true),
			OmitConnResetSession: true,
			OmitConnPrepare:      true,
			OmitRows:             true,
		}),
	}

	db, err := otelsql.Open(driver.driverName, dsn, options...)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s database: %w", driver.Name, err)
	}

	db.SetMaxOpenConns(cfg.GetInt("sql.max_open_conns"))
	db.SetMaxIdleConns(cfg.GetIntOrDefault("sql.max_idle_conns", DefaultMaxIdleConns))
	db.SetConnMaxLifetime(cfg.GetDuration("sql.conn_max_lifetime"))
	db.SetConnMaxIdleTime(cfg.GetDuration("sql.conn_max_idle_time"))

	registration, err := otelsql.RegisterDBStatsMetrics(db, options...)
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("cannot register the database metrics: %w", err)
	}

	params.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !cfg.GetBoolOrDefault("sql.ping_on_start", true) {
				return nil
			}

			if err := db.PingContext(ctx); err != nil {
				return fmt.Errorf("cannot connect to the %s database: %w", driver.Name, err)
			}

			return nil
		},
		OnStop: func(context.Context) error {
			_ = registration.Unregister()

			return db.Close()
		},
	})

	return db, nil
}

// DefaultMaxIdleConns is the default of sql.max_idle_conns, the database/sql default.
const DefaultMaxIdleConns = 2

// Supported values of sql.driver.
const (
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql"
	DriverSQLite   = "sqlite"
)

// Driver describes a supported database.
type Driver struct {
	// Name is the sql.driver value: postgres, mysql or sqlite.
	Name string

	driverName string
	system     attribute.KeyValue
	dialect    string
}

var drivers = map[string]Driver{
	DriverPostgres: {Name: DriverPostgres, driverName: "pgx", system: semconv.DBSystemNamePostgreSQL, dialect: "postgres"},
	DriverMySQL:    {Name: DriverMySQL, driverName: "mysql", system: semconv.DBSystemNameMySQL, dialect: "mysql"},
	DriverSQLite:   {Name: DriverSQLite, driverName: "sqlite", system: semconv.DBSystemNameSQLite, dialect: "sqlite3"},
}

// NewDriver returns the [Driver] configured with sql.driver.
func NewDriver(cfg *config.Config) (Driver, error) {
	return DriverOf(cfg.GetString("sql.driver"))
}

// DriverOf returns the [Driver] of a sql.driver value.
func DriverOf(name string) (Driver, error) {
	driver, ok := drivers[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Driver{}, fmt.Errorf("unsupported sql.driver %q, expected postgres, mysql or sqlite", name)
	}

	return driver, nil
}

// spanName names the spans after the SQL operation (SELECT, INSERT...), or the database/sql
// method (sql.conn.begin_tx...) for calls without query.
func spanName(_ context.Context, method otelsql.Method, query string) string {
	if operation, _, _ := strings.Cut(strings.TrimSpace(query), " "); operation != "" {
		return strings.ToUpper(operation)
	}

	return string(method)
}
