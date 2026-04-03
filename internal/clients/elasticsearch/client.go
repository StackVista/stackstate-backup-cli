// Package elasticsearch provides a client for interacting with Elasticsearch
// including snapshot management, index operations, and SLM policy configuration.
package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
)

// Restore status constants
const (
	StatusSuccess    = "SUCCESS"
	StatusInProgress = "IN_PROGRESS"
)

// Client represents an Elasticsearch client
type Client struct {
	es *elasticsearch.Client
}

// IndexInfo represents detailed information about an Elasticsearch index
type IndexInfo struct {
	Health       string `json:"health"`
	Status       string `json:"status"`
	Index        string `json:"index"`
	UUID         string `json:"uuid"`
	Pri          string `json:"pri"`
	Rep          string `json:"rep"`
	DocsCount    string `json:"docs.count"`
	DocsDeleted  string `json:"docs.deleted"`
	StoreSize    string `json:"store.size"`
	PriStoreSize string `json:"pri.store.size"`
	DatasetSize  string `json:"dataset.size"`
}

// SnapshotFailure represents a shard-level failure in an Elasticsearch snapshot
type SnapshotFailure struct {
	Index     string `json:"index"`
	IndexUUID string `json:"index_uuid"`
	ShardID   int    `json:"shard_id"`
	Reason    string `json:"reason"`
	NodeID    string `json:"node_id"`
	Status    string `json:"status"`
}

// Snapshot represents an Elasticsearch snapshot
type Snapshot struct {
	Snapshot         string            `json:"snapshot"`
	UUID             string            `json:"uuid"`
	Repository       string            `json:"repository"`
	State            string            `json:"state"`
	StartTime        string            `json:"start_time"`
	StartTimeMillis  int64             `json:"start_time_in_millis"`
	EndTime          string            `json:"end_time"`
	EndTimeMillis    int64             `json:"end_time_in_millis"`
	DurationInMillis int64             `json:"duration_in_millis"`
	Indices          []string          `json:"indices"`
	Failures         []SnapshotFailure `json:"failures"`
	Shards           struct {
		Total      int `json:"total"`
		Failed     int `json:"failed"`
		Successful int `json:"successful"`
	} `json:"shards"`
}

// SnapshotsResponse represents the response from Elasticsearch snapshots API
type SnapshotsResponse struct {
	Snapshots []Snapshot `json:"snapshots"`
	Total     int        `json:"total"`
	Remaining int        `json:"remaining"`
}

// NewClient creates a new Elasticsearch client
func NewClient(baseURL string) (*Client, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{baseURL},
	}

	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Elasticsearch client: %w", err)
	}

	return &Client{
		es: es,
	}, nil
}

// ListSnapshots retrieves all snapshots from a repository
func (c *Client) ListSnapshots(repository string) ([]Snapshot, error) {
	res, err := c.es.Snapshot.Get(
		repository,
		[]string{"_all"},
		c.es.Snapshot.Get.WithContext(context.Background()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshots: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var snapshotsResp SnapshotsResponse
	if err := json.NewDecoder(res.Body).Decode(&snapshotsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return snapshotsResp.Snapshots, nil
}

// GetSnapshot retrieves details of a specific snapshot including its indices
func (c *Client) GetSnapshot(repository, snapshotName string) (*Snapshot, error) {
	res, err := c.es.Snapshot.Get(
		repository,
		[]string{snapshotName},
		c.es.Snapshot.Get.WithContext(context.Background()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var snapshotsResp SnapshotsResponse
	if err := json.NewDecoder(res.Body).Decode(&snapshotsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(snapshotsResp.Snapshots) == 0 {
		return nil, fmt.Errorf("snapshot %s not found", snapshotName)
	}

	return &snapshotsResp.Snapshots[0], nil
}

// ListIndices retrieves all indices matching a pattern
func (c *Client) ListIndices(pattern string) ([]string, error) {
	res, err := c.es.Cat.Indices(
		c.es.Cat.Indices.WithContext(context.Background()),
		c.es.Cat.Indices.WithIndex(pattern),
		c.es.Cat.Indices.WithH("index"),
		c.es.Cat.Indices.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var indices []struct {
		Index string `json:"index"`
	}
	if err := json.NewDecoder(res.Body).Decode(&indices); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	result := make([]string, len(indices))
	for i, idx := range indices {
		result[i] = idx.Index
	}

	return result, nil
}

// ListIndicesDetailed retrieves detailed information about all indices
func (c *Client) ListIndicesDetailed() ([]IndexInfo, error) {
	res, err := c.es.Cat.Indices(
		c.es.Cat.Indices.WithContext(context.Background()),
		c.es.Cat.Indices.WithH("health,status,index,uuid,pri,rep,docs.count,docs.deleted,store.size,pri.store.size,dataset.size"),
		c.es.Cat.Indices.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var indices []IndexInfo
	if err := json.NewDecoder(res.Body).Decode(&indices); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return indices, nil
}

// DeleteIndex deletes a specific index
func (c *Client) DeleteIndex(index string) error {
	res, err := c.es.Indices.Delete(
		[]string{index},
		c.es.Indices.Delete.WithContext(context.Background()),
	)
	if err != nil {
		return fmt.Errorf("failed to delete index: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// IndexExists checks if an index exists
func (c *Client) IndexExists(index string) (bool, error) {
	res, err := c.es.Indices.Exists(
		[]string{index},
		c.es.Indices.Exists.WithContext(context.Background()),
	)
	if err != nil {
		return false, fmt.Errorf("failed to check index existence: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}

	if res.IsError() {
		return false, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return true, nil
}

// RolloverDatastream performs a rollover on a datastream
func (c *Client) RolloverDatastream(datastreamName string) error {
	res, err := c.es.Indices.Rollover(
		datastreamName,
		c.es.Indices.Rollover.WithContext(context.Background()),
	)
	if err != nil {
		return fmt.Errorf("failed to rollover datastream: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// DeleteSnapshotRepository deletes a snapshot repository
func (c *Client) DeleteSnapshotRepository(name string) error {
	res, err := c.es.Snapshot.DeleteRepository(
		[]string{name},
		c.es.Snapshot.DeleteRepository.WithContext(context.Background()),
	)
	if err != nil {
		return fmt.Errorf("failed to delete snapshot repository: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil // Repository doesn't exist, which is fine
	}

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// ConfigureSnapshotRepository configures an S3 snapshot repository
func (c *Client) ConfigureSnapshotRepository(name, bucket, endpoint, basePath, accessKey, secretKey string) error {
	body := map[string]interface{}{
		"type": "s3",
		"settings": map[string]interface{}{
			"bucket":            bucket,
			"region":            "minio",
			"endpoint":          endpoint,
			"base_path":         basePath,
			"protocol":          "http",
			"access_key":        accessKey,
			"secret_key":        secretKey,
			"path_style_access": "true",
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	res, err := c.es.Snapshot.CreateRepository(
		name,
		strings.NewReader(string(bodyJSON)),
		c.es.Snapshot.CreateRepository.WithContext(context.Background()),
	)
	if err != nil {
		return fmt.Errorf("failed to create snapshot repository: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// ConfigureSLMPolicy configures a Snapshot Lifecycle Management policy
func (c *Client) ConfigureSLMPolicy(name, schedule, snapshotName, repository, indices, expireAfter string, minCount, maxCount int) error {
	body := map[string]interface{}{
		"schedule":   schedule,
		"name":       snapshotName,
		"repository": repository,
		"config": map[string]interface{}{
			"indices":              indices,
			"ignore_unavailable":   false,
			"include_global_state": false,
		},
		"retention": map[string]interface{}{
			"expire_after": expireAfter,
			"min_count":    minCount,
			"max_count":    maxCount,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	res, err := c.es.SlmPutLifecycle(
		name,
		c.es.SlmPutLifecycle.WithContext(context.Background()),
		c.es.SlmPutLifecycle.WithBody(strings.NewReader(string(bodyJSON))),
	)
	if err != nil {
		return fmt.Errorf("failed to create SLM policy: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// RestoreSnapshot restores a snapshot from a repository asynchronously
// The restore is triggered and returns immediately (waitForCompletion=false)
// Use GetRestoreStatus to check the progress of the restore operation
func (c *Client) RestoreSnapshot(repository, snapshotName, indicesPattern string, partial bool) error {
	body := map[string]interface{}{
		"indices": indicesPattern,
		"partial": partial,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	res, err := c.es.Snapshot.Restore(
		repository,
		snapshotName,
		c.es.Snapshot.Restore.WithContext(context.Background()),
		c.es.Snapshot.Restore.WithBody(strings.NewReader(string(bodyJSON))),
		c.es.Snapshot.Restore.WithWaitForCompletion(false),
	)
	if err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	return nil
}

// RecoveryInfo represents the recovery status of a shard from _cat/recovery API
type RecoveryInfo struct {
	Index      string `json:"index"`
	Shard      string `json:"shard"`
	Type       string `json:"type"`
	Stage      string `json:"stage"`
	Repository string `json:"repository"`
	Snapshot   string `json:"snapshot"`
}

// GetRestoreStatus checks the status of a restore operation by examining active shard recoveries.
// When a snapshot is being restored, shards are recovered with type "snapshot".
// Returns: (statusMessage, isComplete, error)
// Status can be: "IN_PROGRESS", "SUCCESS"
func (c *Client) GetRestoreStatus(repository, snapshotName string) (string, bool, error) {
	// Use _cat/recovery API to check for active snapshot recoveries.
	// This shows shards that are currently being recovered from a snapshot.
	res, err := c.es.Cat.Recovery(
		c.es.Cat.Recovery.WithContext(context.Background()),
		c.es.Cat.Recovery.WithFormat("json"),
		c.es.Cat.Recovery.WithActiveOnly(true),
		c.es.Cat.Recovery.WithH("index,shard,type,stage,repository,snapshot"),
	)
	if err != nil {
		return "", false, fmt.Errorf("failed to get recovery status: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return "", false, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var recoveries []RecoveryInfo
	if err := json.NewDecoder(res.Body).Decode(&recoveries); err != nil {
		return "", false, fmt.Errorf("failed to decode response: %w", err)
	}

	// Check if any active recovery is from the specified snapshot
	for _, recovery := range recoveries {
		if recovery.Type == "snapshot" &&
			recovery.Repository == repository &&
			recovery.Snapshot == snapshotName {
			return StatusInProgress, false, nil
		}
	}

	// No active recoveries from this snapshot - restore is complete
	return StatusSuccess, true, nil
}
