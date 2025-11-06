package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Options customises how EXPLAIN is executed.
type Options struct {
	Timeout time.Duration
	Trace   bool
}

// Run executes EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) for the provided SQL statement.
func Run(ctx context.Context, dsn, sqlStatement string, opts Options) ([]byte, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("runner: empty DSN")
	}
	query := strings.TrimSpace(sqlStatement)
	if query == "" {
		return nil, fmt.Errorf("runner: empty sql statement")
	}

	explainSQL := fmt.Sprintf("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) %s", query)

	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("runner: connect: %w", err)
	}
	defer conn.Close(ctx)

	var payload []byte
	if err := conn.QueryRow(ctx, explainSQL).Scan(&payload); err != nil {
		return nil, fmt.Errorf("runner: query: %w", err)
	}
	return payload, nil
}
