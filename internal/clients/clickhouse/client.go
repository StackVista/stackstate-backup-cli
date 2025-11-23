package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	clickhouseDriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	// defaultRestoreOperationTimeout is the timeout for waiting for restore operation to be created
	defaultRestoreOperationTimeout = 30 * time.Second
	// defaultRestoreOperationPollInterval is the interval between checks for restore operation
	defaultRestoreOperationPollInterval = 2 * time.Second
)

// Client represents a ClickHouse Backup API client with optional SQL support
type Client struct {
	backupAPIURL        string
	backupAPIHTTPClient *http.Client
	clickhouseAddr      string
	clickhouseDatabase  string
	clickhouseUsername  string
	clickhousePassword  string
}

// Backup represents a ClickHouse backup from the API
type Backup struct {
	Name           string `json:"name"`
	Created        string `json:"created"`
	Size           int64  `json:"size"`
	DataSize       int64  `json:"data_size"`
	MetadataSize   int64  `json:"metadata_size"`
	CompressedSize int64  `json:"compressed_size"`
	Location       string `json:"location"`
	Required       string `json:"required"`
	Desc           string `json:"desc"`
}

// RestoreAction represents a restore action from the backup API
type RestoreAction struct {
	Command     string `json:"command"`
	Start       string `json:"start"`
	Finish      string `json:"finish"`
	Status      string `json:"status"` // "in progress", "success", "error"
	Error       string `json:"error"`
	OperationID string `json:"operation_id"`
}

// NewClient creates a new ClickHouse client with both Backup API and SQL support
func NewClient(backupAPI, addr, db, username, password string) (*Client, error) {
	if backupAPI == "" {
		return nil, fmt.Errorf("backupAPIURL cannot be empty")
	}
	if addr == "" {
		return nil, fmt.Errorf("clickhouseAddr cannot be empty")
	}
	if db == "" {
		return nil, fmt.Errorf("clickhouseDatabase cannot be empty")
	}
	if username == "" {
		return nil, fmt.Errorf("clickhouseUsername cannot be empty")
	}
	if password == "" {
		return nil, fmt.Errorf("clickhousePassword cannot be empty")
	}

	return &Client{
		backupAPIURL: backupAPI,
		backupAPIHTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		clickhouseAddr:     addr,
		clickhouseDatabase: db,
		clickhouseUsername: username,
		clickhousePassword: password,
	}, nil
}

// ListBackups retrieves all backups from ClickHouse Backup API
// The API returns newline-delimited JSON (NDJSON) format
func (c *Client) ListBackups() ([]Backup, error) {
	url := fmt.Sprintf("%s/backup/list", c.backupAPIURL)

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backup API returned status %d", resp.StatusCode)
	}

	// Parse NDJSON response (newline-delimited JSON)
	var backups []Backup
	dec := json.NewDecoder(resp.Body)
	for {
		var backup Backup
		if err := dec.Decode(&backup); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		backups = append(backups, backup)
	}

	return backups, nil
}

// TriggerRestore initiates a restore operation via HTTP POST and returns the restore operation ID
// POST /backup/download/${BACKUP_NAME}?callback=http://localhost:{port}/backup/restore/${BACKUP_NAME}
// Note: The initial response contains the download operation ID, but we need to poll for the restore operation ID
func (c *Client) TriggerRestore(backupName string) (string, error) {
	callbackURL := fmt.Sprintf("%s/backup/restore/%s", c.backupAPIURL, backupName)
	reqURL := fmt.Sprintf("%s/backup/download/%s?callback=%s", c.backupAPIURL, backupName, url.QueryEscape(callbackURL))

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to trigger restore: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("restore API returned status %d", resp.StatusCode)
	}

	// Parse response to get download operation ID (not the restore operation ID)
	var downloadAction RestoreAction
	if err := json.NewDecoder(resp.Body).Decode(&downloadAction); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// Poll for the restore operation (command contains "restore" not "download")
	// The restore is triggered via callback after download completes, so we need to wait
	return c.waitForRestoreOperationID(backupName, defaultRestoreOperationTimeout, defaultRestoreOperationPollInterval)
}

// waitForRestoreOperationID polls for the restore operation ID with timeout and retry
func (c *Client) waitForRestoreOperationID(backupName string, timeout, pollInterval time.Duration) (string, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return "", fmt.Errorf("timeout waiting for restore operation to be created for backup: %s", backupName)
		case <-ticker.C:
			operationID, err := c.getRestoreOperationID(backupName)
			if err == nil {
				return operationID, nil
			}
			// Continue polling on error (restore might not be created yet)
		}
	}
}

// getRestoreOperationID polls for the restore operation ID for a given backup
// It looks for the most recent restore action (not download) matching the backup name
func (c *Client) getRestoreOperationID(backupName string) (string, error) {
	reqURL := fmt.Sprintf("%s/backup/actions?filter=restore", c.backupAPIURL)

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get actions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("actions API returned %d", resp.StatusCode)
	}

	// Parse NDJSON response
	var actions []RestoreAction
	dec := json.NewDecoder(resp.Body)
	for {
		var action RestoreAction
		if err := dec.Decode(&action); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("failed to decode: %w", err)
		}
		// Look for restore command (not download) matching the backup name
		if action.Command == fmt.Sprintf("restore %s", backupName) {
			actions = append(actions, action)
		}
	}

	if len(actions) == 0 {
		return "", fmt.Errorf("no restore operation found for backup: %s", backupName)
	}

	// Return most recent action's operation ID (last in list)
	return actions[len(actions)-1].OperationID, nil
}

// GetRestoreStatus retrieves the current restore status for a specific backup
// GET /backup/actions?filter=restore
// Returns the most recent restore action matching the operation id
func (c *Client) GetRestoreStatus(operationID string) (*RestoreAction, error) {
	reqURL := fmt.Sprintf("%s/backup/actions?filter=restore", c.backupAPIURL)

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status API returned %d", resp.StatusCode)
	}

	// Parse NDJSON response (newline-delimited JSON)
	var actions []RestoreAction
	dec := json.NewDecoder(resp.Body)
	for {
		var action RestoreAction
		if err := dec.Decode(&action); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode: %w", err)
		}
		// Filter by operation id
		if action.OperationID == operationID {
			actions = append(actions, action)
		}
	}

	if len(actions) == 0 {
		return nil, fmt.Errorf("no restore action found for operation id: %s", operationID)
	}

	// Return most recent action (last in list)
	return &actions[len(actions)-1], nil
}

// WaitForRestoreCompletion polls until restore completes or times out
func (c *Client) WaitForRestoreCompletion(operationID string, timeout, pollInterval time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for restore to complete")
		case <-ticker.C:
			status, err := c.GetRestoreStatus(operationID)
			if err != nil {
				// Continue polling on error (might be transient)
				continue
			}

			if status.Status == "success" {
				return nil
			}

			if status.Status == "error" {
				return fmt.Errorf("restore failed: %s", status.Error)
			}

			// Status is "in progress" - continue polling
		}
	}
}

// Connect opens a connection to ClickHouse instance
func (c *Client) Connect() (driver.Conn, func() error, error) {
	// Create ClickHouse SQL connection
	conn, err := clickhouseDriver.Open(&clickhouseDriver.Options{
		Addr: []string{c.clickhouseAddr},
		Auth: clickhouseDriver.Auth{
			Database: c.clickhouseDatabase,
			Username: c.clickhouseUsername,
			Password: c.clickhousePassword,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to ClickHouse: %w", err)
	}

	return conn, conn.Close, nil
}
