package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	clickhouseDriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

const (
	defaultHTTPClientTimeout     = 30 * time.Second
	defaultOperationTimeout      = 30 * time.Second
	defaultOperationPollInterval = 2 * time.Second
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

// ListBackupResponse represents a ClickHouse backup from the API
type ListBackupResponse struct {
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

// ActionResponse represents a action from the backup API
type ActionResponse struct {
	Command     string `json:"command"`
	Start       string `json:"start"`
	Finish      string `json:"finish"`
	Status      string `json:"status"`
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
			Timeout: defaultHTTPClientTimeout,
		},
		clickhouseAddr:     addr,
		clickhouseDatabase: db,
		clickhouseUsername: username,
		clickhousePassword: password,
	}, nil
}

// ListBackups retrieves all backups from ClickHouse Backup API
// The API returns newline-delimited JSON (NDJSON) format
func (c *Client) ListBackups(ctx context.Context) ([]ListBackupResponse, error) {
	listURL := fmt.Sprintf("%s/backup/list", c.backupAPIURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backup API returned status %d", resp.StatusCode)
	}

	// Parse NDJSON response (newline-delimited JSON)
	backups, err := parseNDJSONBackups(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse backups: %w", err)
	}

	return backups, nil
}

// TriggerRestore initiates a restore operation via HTTP POST and returns the restore operation ID
func (c *Client) TriggerRestore(ctx context.Context, backupName string) (string, error) {
	downloadURL := fmt.Sprintf("%s/backup/download/%s", c.backupAPIURL, backupName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to trigger restore: %w", err)
	}
	var downloadAction ActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&downloadAction); err != nil {
		return "", fmt.Errorf("failed to decode %s response: %w", downloadURL, err)
	}
	if err = resp.Body.Close(); err != nil {
		return "", fmt.Errorf("failed to close response body: %w", err)
	}

	_, err = c.waitForAction(ctx, downloadAction.OperationID, defaultOperationTimeout, defaultOperationPollInterval)
	if err != nil {
		return "", fmt.Errorf("failed to download backup: %w", err)
	}

	restoreURL := fmt.Sprintf("%s/backup/restore/%s", c.backupAPIURL, backupName)
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, restoreURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err = c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to trigger restore: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("restore API returned status %d", resp.StatusCode)
	}
	var restoreAction ActionResponse
	if err := json.NewDecoder(resp.Body).Decode(&restoreAction); err != nil {
		return "", fmt.Errorf("failed to decode %s response: %w", restoreURL, err)
	}
	if err = resp.Body.Close(); err != nil {
		return "", fmt.Errorf("failed to close response body: %w", err)
	}

	return restoreAction.OperationID, nil
}

// waitForAction polls for the restore operation ID with timeout and retry
func (c *Client) waitForAction(ctx context.Context, operationID string, timeout, pollInterval time.Duration) (*ActionResponse, error) {
	// Create a child context with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Could be timeout OR cancellation
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("timeout waiting for operation to finish: %s", operationID)
			}
			return nil, fmt.Errorf("operation cancelled: %w", ctx.Err())
		case <-ticker.C:
			action, err := c.GetRestoreStatus(ctx, operationID)
			if err != nil {
				return nil, fmt.Errorf("fail to get action with operation id: %s", operationID)
			}
			if action.Status == "success" || action.Status == "error" {
				return action, nil
			}
			// Status is "in progress" - continue polling
		}
	}
}

// GetRestoreStatus polls for the restore operation ID with timeout and retry
func (c *Client) GetRestoreStatus(ctx context.Context, operationID string) (*ActionResponse, error) {
	actionURL := fmt.Sprintf("%s/backup/status?operation_id=%s", c.backupAPIURL, operationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, actionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.backupAPIHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request operation status: %w", err)
	}

	// Parse NDJSON response
	allActions, err := parseNDJSONActions(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse operation status response: %w", err)
	}
	if err = resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("failed to close response body: %w", err)
	}

	// It should be exactly one action
	if lenActions := len(allActions); lenActions != 1 {
		return nil, fmt.Errorf("incorrect operation status response, expected one record for operation, got: %d", lenActions)
	}

	return &allActions[0], nil
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

// parseNDJSONBackups parses newline-delimited JSON into ListBackupResponse slice
func parseNDJSONBackups(body io.Reader) ([]ListBackupResponse, error) {
	var backups []ListBackupResponse
	dec := json.NewDecoder(body)
	for {
		var backup ListBackupResponse
		if err := dec.Decode(&backup); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode NDJSON: %w", err)
		}
		backups = append(backups, backup)
	}
	return backups, nil
}

// parseNDJSONActions parses newline-delimited JSON into ActionResponse slice
func parseNDJSONActions(body io.Reader) ([]ActionResponse, error) {
	var actions []ActionResponse
	dec := json.NewDecoder(body)
	for {
		var action ActionResponse
		if err := dec.Decode(&action); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("failed to decode NDJSON: %w", err)
		}
		actions = append(actions, action)
	}
	return actions, nil
}
