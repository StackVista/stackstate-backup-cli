package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/clickhouse"
	"github.com/stackvista/stackstate-backup-cli/cmd/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/cmd/settings"
	"github.com/stackvista/stackstate-backup-cli/cmd/stackgraph"
	"github.com/stackvista/stackstate-backup-cli/cmd/version"
	"github.com/stackvista/stackstate-backup-cli/cmd/victoriametrics"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

var (
	flags *config.CLIGlobalFlags
)

// addBackupConfigFlags adds configuration flags needed for backup/restore operations
// to commands that interact with data services (Elasticsearch, etc.)
func addBackupConfigFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVarP(&flags.Namespace, "namespace", "n", "", "Kubernetes namespace (required)")
	cmd.PersistentFlags().StringVar(&flags.Kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.PersistentFlags().BoolVar(&flags.Debug, "debug", false, "Enable debug output")
	cmd.PersistentFlags().BoolVarP(&flags.Quiet, "quiet", "q", false, "Suppress operational messages (only show errors and data output)")
	cmd.PersistentFlags().StringVar(&flags.ConfigMapName, "configmap", "suse-observability-backup-config", "ConfigMap name containing backup configuration")
	cmd.PersistentFlags().StringVar(&flags.SecretName, "secret", "suse-observability-backup-config", "Secret name containing backup configuration")
	cmd.PersistentFlags().StringVarP(&flags.OutputFormat, "output", "o", "table", "Output format (table, json)")
	_ = cmd.MarkPersistentFlagRequired("namespace")
}

func init() {
	flags = config.NewCLIGlobalFlags()

	// Add backup config flags to commands that need them
	esCmd := elasticsearch.Cmd(flags)
	addBackupConfigFlags(esCmd)
	rootCmd.AddCommand(esCmd)

	stackgraphCmd := stackgraph.Cmd(flags)
	addBackupConfigFlags(stackgraphCmd)
	rootCmd.AddCommand(stackgraphCmd)

	settingsCmd := settings.Cmd(flags)
	addBackupConfigFlags(settingsCmd)
	rootCmd.AddCommand(settingsCmd)

	victoriaMetricsCmd := victoriametrics.Cmd(flags)
	addBackupConfigFlags(victoriaMetricsCmd)
	rootCmd.AddCommand(victoriaMetricsCmd)

	clickhouseCmd := clickhouse.NewClickhouseCmd(flags)
	addBackupConfigFlags(clickhouseCmd)
	rootCmd.AddCommand(clickhouseCmd)

	// Add commands that don't need backup config flags
	rootCmd.AddCommand(version.Cmd())
}

var rootCmd = &cobra.Command{
	Use:   "sts-backup",
	Short: "Backup and restore tool for SUSE Observability platform",
	Long:  `A CLI tool for managing backups and restores for SUSE Observability platform running on Kubernetes.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
