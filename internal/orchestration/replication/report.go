// Package replication checks the observed replication state of chart-managed databases.
// It does not grant permission to disrupt a node or modify database state.
package replication

import (
	"fmt"
	"slices"
	"time"
)

const (
	Healthy       = "healthy"
	Degraded      = "degraded"
	Unknown       = "unknown"
	NotApplicable = "not_applicable"

	minReplicas = 2
)

var supportedComponents = []string{"hdfs", "elasticsearch", "kafka", "clickhouse", "zookeeper"}

// Options identifies the installation and bounds each database query.
type Options struct {
	Namespace             string
	Components            []string
	RequestTimeout        time.Duration
	KafkaClientProperties string
	KafkaBootstrapServer  string
	ElasticsearchScheme   string
	ElasticsearchCA       string
	ElasticsearchHost     string
}

// Validate rejects ambiguous scope and unsupported probe settings.
func (o Options) Validate() error {
	if o.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	if o.RequestTimeout <= 0 {
		return fmt.Errorf("request-timeout must be positive")
	}
	if o.ElasticsearchScheme != "http" && o.ElasticsearchScheme != "https" {
		return fmt.Errorf("elasticsearch-scheme must be http or https")
	}
	if len(o.Components) == 0 {
		return fmt.Errorf("select at least one component")
	}
	seen := make(map[string]bool)
	for _, component := range o.Components {
		if !slices.Contains(supportedComponents, component) || seen[component] {
			return fmt.Errorf("invalid or duplicate component %q; choose from %v", component, supportedComponents)
		}
		seen[component] = true
	}
	return nil
}

// Result reports one component; missing evidence is unknown, never healthy.
type Result struct {
	Component string   `json:"component"`
	Status    string   `json:"status"`
	Messages  []string `json:"messages"`
}

// Report is a point-in-time observation, not a maintenance lock.
type Report struct {
	CheckedAt time.Time `json:"checkedAt"`
	Namespace string    `json:"namespace"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Checks    []Result  `json:"checks"`
	topology  string
}

func result(component, status, message string) Result {
	return Result{Component: component, Status: status, Messages: []string{message}}
}

func reportStatus(checks []Result) string {
	status := NotApplicable
	if len(checks) == 0 {
		return Unknown
	}
	for _, check := range checks {
		if check.Status == Unknown {
			return Unknown
		}
		if check.Status == NotApplicable {
			continue
		}
		if check.Status != Healthy {
			status = Degraded
		} else if status == NotApplicable {
			status = Healthy
		}
	}
	return status
}
