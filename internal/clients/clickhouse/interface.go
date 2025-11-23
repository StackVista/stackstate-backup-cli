package clickhouse

import (
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Interface defines the contract for ClickHouse Backup API client operations
type Interface interface {
	// ListBackups retrieves all backups from ClickHouse Backup API
	ListBackups() ([]Backup, error)

	// TriggerRestore initiates a restore operation and returns the operation ID
	TriggerRestore(backupName string) (string, error)

	// GetRestoreStatus retrieves the current restore status
	GetRestoreStatus(operationID string) (*RestoreAction, error)

	// WaitForRestoreCompletion polls until restore completes or times out
	WaitForRestoreCompletion(operationID string, timeout, pollInterval time.Duration) error

	// Connect opens connection to a ClickHouse database
	Connect() (driver.Conn, func() error, error)
}

// Ensure *Client implements Interface
var _ Interface = (*Client)(nil)
