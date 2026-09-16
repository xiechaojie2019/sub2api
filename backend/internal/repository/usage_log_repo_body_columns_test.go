package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogInsertQueriesKeepBodyColumnsCommaSeparated(t *testing.T) {
	log := &service.UsageLog{
		UserID:    1,
		APIKeyID:  2,
		AccountID: 3,
		RequestID: "req-body-columns",
		Model:     "gpt-5",
		CreatedAt: time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	batchQuery, _ := buildUsageLogBatchInsertQuery(
		[]string{usageLogBatchKey(log.RequestID, log.APIKeyID)},
		map[string]usageLogInsertPrepared{usageLogBatchKey(log.RequestID, log.APIKeyID): prepared},
	)
	bestEffortQuery, _ := buildUsageLogBestEffortInsertQuery([]usageLogInsertPrepared{prepared})

	for name, query := range map[string]string{
		"batch":       batchQuery,
		"best_effort": bestEffortQuery,
	} {
		t.Run(name, func(t *testing.T) {
			require.NotContains(t, query, "created_at\n\t\t\trequest_body")
			require.Contains(t, query, "created_at,\n\t\t\trequest_body")
			require.Contains(t, query, "request_body,\n\t\t\tresponse_body")
			require.NotContains(t, strings.ReplaceAll(query, "\r\n", "\n"), "response_body\n\t\t\trequest_body_truncated")
		})
	}
}
