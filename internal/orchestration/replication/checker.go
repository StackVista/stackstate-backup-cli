package replication

import (
	"context"
	"fmt"
	"time"
)

// Checker observes databases without invoking backup or restore operations.
type Checker struct {
	kube    Kubernetes
	options Options
}

// New creates a checker for the installation in one namespace.
func New(kube Kubernetes, options Options) (*Checker, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if options.KafkaBootstrapServer == "" {
		options.KafkaBootstrapServer = "localhost:9092"
	}
	if options.ElasticsearchHost == "" {
		options.ElasticsearchHost = "127.0.0.1"
	}
	return &Checker{kube: kube, options: options}, nil
}

// Check queries every selected component and rechecks Kubernetes membership afterward.
func (c *Checker) Check(ctx context.Context) Report {
	report := Report{
		CheckedAt: time.Now().UTC(), Namespace: c.options.Namespace,
		Checks: make([]Result, 0, len(c.options.Components)),
	}
	before, err := c.discover(ctx)
	for _, component := range c.options.Components {
		if err != nil {
			report.Checks = append(report.Checks, result(component, Unknown, err.Error()))
			continue
		}
		report.Checks = append(report.Checks, c.checkComponent(ctx, before, component))
	}
	if err == nil {
		after, afterErr := c.discover(ctx)
		if afterErr != nil || before.fingerprint() != after.fingerprint() {
			message := "Kubernetes membership or status changed during the checks; repeat the observation"
			if afterErr != nil {
				message = afterErr.Error()
			}
			for n := range report.Checks {
				report.Checks[n].Status = Unknown
				report.Checks[n].Messages = append(report.Checks[n].Messages, message)
			}
		}
	}
	report.Status = reportStatus(report.Checks)
	return report
}

func (c *Checker) query(ctx context.Context, pod, container string, command []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.options.RequestTimeout)
	defer cancel()
	return c.kube.Exec(ctx, c.options.Namespace, pod, container, command)
}

func (c *Checker) checkComponent(ctx context.Context, inventory inventory, component string) Result {
	switch component {
	case "hdfs":
		return c.checkHDFS(ctx, inventory)
	case "elasticsearch":
		return c.checkElasticsearch(ctx, inventory)
	case "kafka":
		return c.checkKafka(ctx, inventory)
	case "clickhouse":
		return c.checkClickHouse(ctx, inventory)
	case "zookeeper":
		return c.checkZooKeeper(ctx, inventory)
	default:
		return result(component, Unknown, "unsupported component")
	}
}

func expectedMembers(members []member) error {
	if len(members) < minReplicas {
		return fmt.Errorf("at least %d application replicas are required; discovered %d", minReplicas, len(members))
	}
	return nil
}
