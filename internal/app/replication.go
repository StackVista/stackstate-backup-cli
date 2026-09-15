package app

import (
	"fmt"

	"github.com/stackvista/stackstate-backup-cli/internal/clients/k8s"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/replication"
)

// NewReplicationChecker requires Kubernetes access, but no backup ConfigMap or Secret.
func NewReplicationChecker(kubeconfig string, options replication.Options) (*replication.Checker, error) {
	client, err := k8s.NewClient(kubeconfig, false)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return replication.New(client, options)
}
