// Package replication exposes read-only database replication checks.
package replication

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/stackvista/stackstate-backup-cli/internal/app"
	checker "github.com/stackvista/stackstate-backup-cli/internal/orchestration/replication"
)

const (
	defaultTimeout        = 10 * time.Minute
	defaultRequestTimeout = 30 * time.Second
	defaultInterval       = 10 * time.Second
	defaultStableFor      = 30 * time.Second
	tablePadding          = 2
)

type flags struct {
	options    checker.Options
	kubeconfig string
	output     string
	wait       bool
	timeout    time.Duration
	interval   time.Duration
	stableFor  time.Duration
}

// Cmd creates the replication command independently of backup configuration.
func Cmd() *cobra.Command {
	command := &cobra.Command{Use: "replication", Short: "Inspect database replication without changing cluster state"}
	f := &flags{}
	check := &cobra.Command{
		Use: "check", Short: "Check observed replication; return nonzero unless all selected checks pass",
		Long: "Check chart-managed HDFS, Elasticsearch, Kafka, ClickHouse and ZooKeeper replication. " +
			"This is a point-in-time observation, not permission to remove a node. " +
			"Uses StatefulSet configuration to select applicable checks. " +
			"Requires pods/exec access; only fixed read-only database queries are executed.",
		Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error { return run(command, f) },
	}
	check.Flags().StringVarP(&f.options.Namespace, "namespace", "n", "", "Kubernetes namespace (required)")
	check.Flags().StringVar(&f.kubeconfig, "kubeconfig", "", "Kubeconfig path; uses normal kubeconfig or in-cluster credentials")
	check.Flags().StringSliceVar(&f.options.Components, "components", []string{"hdfs", "elasticsearch", "kafka", "clickhouse", "zookeeper"}, "Components to check")
	check.Flags().StringVarP(&f.output, "output", "o", "table", "Output format: table or json")
	check.Flags().BoolVar(&f.wait, "wait", false, "Wait for sustained healthy replication")
	check.Flags().DurationVar(&f.timeout, "timeout", defaultTimeout, "Overall deadline, including queries")
	check.Flags().DurationVar(&f.options.RequestTimeout, "request-timeout", defaultRequestTimeout, "Deadline for each ordinary Kubernetes request or database probe")
	check.Flags().DurationVar(&f.options.HDFSAuditTimeout, "hdfs-audit-timeout", checker.DefaultAuditTimeout, "Deadline for the final HDFS metadata audit, within the overall timeout (0 uses default)")
	check.Flags().DurationVar(&f.interval, "interval", defaultInterval, "Interval between observations in wait mode")
	check.Flags().DurationVar(&f.stableFor, "stable-for", defaultStableFor, "Required healthy observation period in wait mode")
	check.Flags().StringVar(&f.options.KafkaClientProperties, "kafka-client-properties", "", "Kafka client properties file already mounted in broker pods")
	check.Flags().StringVar(&f.options.KafkaBootstrapServer, "kafka-bootstrap-server", "localhost:9092", "Kafka bootstrap address reachable from the broker pod")
	check.Flags().StringVar(&f.options.ElasticsearchScheme, "elasticsearch-scheme", "http", "Elasticsearch loopback protocol: http or https")
	check.Flags().StringVar(&f.options.ElasticsearchCA, "elasticsearch-ca", "", "CA file already mounted in Elasticsearch pods")
	check.Flags().StringVar(&f.options.ElasticsearchHost, "elasticsearch-server-name", "127.0.0.1", "Elasticsearch TLS server name, resolved to loopback inside the pod")
	_ = check.MarkFlagRequired("namespace")
	command.AddCommand(check)
	return command
}

func (f *flags) validate() error {
	if f.output != "table" && f.output != "json" {
		return fmt.Errorf("output must be table or json")
	}
	if f.timeout <= 0 || f.interval <= 0 || f.stableFor < 0 {
		return fmt.Errorf("timeout and interval must be positive; stable-for cannot be negative")
	}
	if f.wait && f.stableFor >= f.timeout {
		return fmt.Errorf("stable-for must be shorter than timeout")
	}
	return f.options.Validate()
}

func run(command *cobra.Command, f *flags) error {
	if err := f.validate(); err != nil {
		return err
	}
	probe, err := app.NewReplicationChecker(f.kubeconfig, f.options)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(command.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	report, checkErr := observe(ctx, probe.Check, f, command.ErrOrStderr(), probe.Verify)
	return finishReport(command.OutOrStdout(), f.output, report, checkErr)
}

func finishReport(writer io.Writer, format string, report checker.Report, checkErr error) error {
	if checkErr != nil {
		report.Status = checker.Unknown
		report.Error = checkErr.Error()
	}
	if err := writeReport(writer, format, report); err != nil {
		return err
	}
	if checkErr != nil {
		return checkErr
	}
	if report.Status != checker.Healthy && report.Status != checker.NotApplicable {
		return fmt.Errorf("replication is %s; see the report", report.Status)
	}
	return nil
}

func observe(ctx context.Context, check func(context.Context) checker.Report, f *flags, progress io.Writer, verify func(context.Context, checker.Report) checker.Report) (checker.Report, error) {
	if !f.wait {
		report := check(ctx)
		if ctx.Err() != nil {
			return checker.Report{Namespace: report.Namespace}, fmt.Errorf("replication check ended: %w", ctx.Err())
		}
		if verify != nil && report.Status == checker.Healthy {
			_, _ = fmt.Fprintf(progress, "[%s] Running final replication verification.\n", time.Now().UTC().Format(time.RFC3339))
			verified := verify(ctx, report)
			if ctx.Err() != nil {
				return report, fmt.Errorf("replication verification ended: %w", ctx.Err())
			}
			report = verified
		}
		return report, nil
	}
	_, _ = fmt.Fprintf(progress, "[%s] Waiting for all applicable checks to remain healthy for %s (timeout %s).\n",
		time.Now().UTC().Format(time.RFC3339), f.stableFor, f.timeout)
	return checker.Wait(ctx, check, f.interval, f.stableFor, func(report checker.Report, state checker.WaitProgress) {
		timestamp := state.ObservedAt.Format(time.RFC3339)
		for _, check := range report.Checks {
			_, _ = fmt.Fprintf(progress, "[%s] %s %s: %s\n", timestamp, check.Component, check.Status, strings.Join(check.Messages, "; "))
		}
		_, _ = fmt.Fprintf(progress, "[%s] %s\n", timestamp, stabilityMessage(report, state))
	}, verify)
}

func stabilityMessage(report checker.Report, state checker.WaitProgress) string {
	switch {
	case state.Verifying:
		return "All applicable checks stable; running final replication verification."
	case state.RetryAfter > 0:
		return fmt.Sprintf("Final verification did not pass; stability period reset; next observation in %s.", state.RetryAfter)
	case report.Status == checker.NotApplicable:
		return "No applicable replication checks; configured component availability checks passed."
	case state.Reset && report.Status == checker.Healthy:
		return fmt.Sprintf("Database topology, readiness or runtime changed; stability period restarted (0s/%s).", state.Required)
	case state.Complete:
		return fmt.Sprintf("All applicable checks healthy; stability period satisfied (%s/%s).",
			state.HealthyFor.Round(time.Millisecond), state.Required)
	case report.Status == checker.Healthy:
		return fmt.Sprintf("All applicable checks healthy; verifying stability: %s/%s (%s remaining).",
			state.HealthyFor.Round(time.Millisecond), state.Required, (state.Required - state.HealthyFor).Round(time.Millisecond))
	case state.Reset:
		return "Stability period reset; waiting for all selected checks to become healthy."
	default:
		return "Waiting for all selected checks to become healthy; stability period has not started."
	}
}

func writeReport(writer io.Writer, format string, report checker.Report) error {
	if format == "json" {
		if err := json.NewEncoder(writer).Encode(report); err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(writer, "[%s] Replication result: %s\n", time.Now().UTC().Format(time.RFC3339), report.Status); err != nil {
		return fmt.Errorf("write report status: %w", err)
	}
	if report.Error != "" {
		if _, err := fmt.Fprintln(writer, report.Error); err != nil {
			return fmt.Errorf("write report error: %w", err)
		}
	}
	if len(report.Checks) == 0 {
		_, err := fmt.Fprintln(writer, "No completed observation.")
		return err
	}
	if _, err := fmt.Fprintf(writer, "Last completed observation started: %s\n", report.CheckedAt.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("write observation timestamp: %w", err)
	}
	table := tabwriter.NewWriter(writer, 0, 0, tablePadding, ' ', 0)
	if _, err := fmt.Fprintln(table, "COMPONENT\tSTATUS\tDETAILS"); err != nil {
		return fmt.Errorf("write report header: %w", err)
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(table, "%s\t%s\t%s\n", check.Component, check.Status, strings.Join(check.Messages, "; ")); err != nil {
			return fmt.Errorf("write report row: %w", err)
		}
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("flush report: %w", err)
	}
	return nil
}
