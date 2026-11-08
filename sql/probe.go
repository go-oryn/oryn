package sql

import (
	"context"
	stdsql "database/sql"
)

// Probe is a [health.Probe] pinging the database.
type Probe struct {
	db *stdsql.DB
}

// NewProbe returns a [Probe].
func NewProbe(db *stdsql.DB) *Probe {
	return &Probe{db: db}
}

// Name returns "sql".
func (p *Probe) Name() string {
	return ModuleName
}

// Check pings the database.
func (p *Probe) Check(ctx context.Context) error {
	return p.db.PingContext(ctx)
}
