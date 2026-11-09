package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

type recordingExecer struct {
	errs    map[string]error
	queries []string
}

func (r *recordingExecer) Exec(_ context.Context, query string, _ ...any) error {
	r.queries = append(r.queries, query)
	return r.errs[query]
}

const (
	dropMarkCache           = "SYSTEM DROP MARK CACHE ON CLUSTER default"
	dropQueryConditionCache = "SYSTEM DROP QUERY CONDITION CACHE ON CLUSTER default"
)

func TestRunPostRestoreSQL(t *testing.T) {
	syntaxError := &proto.Exception{Code: syntaxErrorCode, Message: "Syntax error"}
	clusterMissing := &proto.Exception{Code: 701, Message: "Requested cluster 'default' not found"}

	tests := []struct {
		name        string
		errs        map[string]error
		wantQueries []string
		wantErr     string
	}{
		{
			name:        "drops mark and query condition caches on every replica",
			wantQueries: []string{dropMarkCache, dropQueryConditionCache},
		},
		{
			name:        "skips query condition cache on ClickHouse without it",
			errs:        map[string]error{dropQueryConditionCache: fmt.Errorf("wrapped: %w", syntaxError)},
			wantQueries: []string{dropMarkCache, dropQueryConditionCache},
		},
		{
			name:        "does not skip a syntax error from a required statement",
			errs:        map[string]error{dropMarkCache: syntaxError},
			wantQueries: []string{dropMarkCache},
			wantErr:     dropMarkCache,
		},
		{
			name:        "fails on other query condition cache errors",
			errs:        map[string]error{dropQueryConditionCache: clusterMissing},
			wantQueries: []string{dropMarkCache, dropQueryConditionCache},
			wantErr:     "cluster 'default' not found",
		},
		{
			name:        "fails on connection errors",
			errs:        map[string]error{dropMarkCache: errors.New("connection reset")},
			wantQueries: []string{dropMarkCache},
			wantErr:     "connection reset",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &recordingExecer{errs: tt.errs}

			err := runPostRestoreSQL(context.Background(), conn, logger.New(true, false))

			assert.Equal(t, tt.wantQueries, conn.queries)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
