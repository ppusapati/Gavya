package tenantdb

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
)

// recordingExecer stands in for a connection so the statement this package
// builds can be inspected rather than inferred.
type recordingExecer struct {
	calls []call
}

type call struct {
	sql  string
	args []any
}

func (r *recordingExecer) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.calls = append(r.calls, call{sql: sql, args: args})
	return pgconn.CommandTag{}, nil
}
