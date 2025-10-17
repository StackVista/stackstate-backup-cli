package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/elasticsearch"
	"github.com/stackvista/stackstate-backup-cli/cmd/version"
	"github.com/stackvista/stackstate-backup-cli/internal/config"
)

var (
	cliCtx *config.Context
)

// addBackupConfigFlags adds configuration flags needed for backup/restore operations
// to commands that interact with data services (Elasticsearch, etc.)
func addBackupConfigFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&cliCtx.Config.Namespace, "namespace", "", "Kubernetes namespace (required)")
	cmd.PersistentFlags().StringVar(&cliCtx.Config.Kubeconfig, "kubeconfig", "", "Path to kubeconfig file (default: ~/.kube/config)")
	cmd.PersistentFlags().BoolVar(&cliCtx.Config.Debug, "debug", false, "Enable debug output")
	cmd.PersistentFlags().BoolVarP(&cliCtx.Config.Quiet, "quiet", "q", false, "Suppress operational messages (only show errors and data output)")
	cmd.PersistentFlags().StringVar(&cliCtx.Config.ConfigMapName, "configmap", "suse-observability-backup-config", "ConfigMap name containing backup configuration")
	cmd.PersistentFlags().StringVar(&cliCtx.Config.SecretName, "secret", "suse-observability-backup-config", "Secret name containing backup configuration")
	cmd.PersistentFlags().StringVarP(&cliCtx.Config.OutputFormat, "output", "o", "table", "Output format (table, json)")
	_ = cmd.MarkPersistentFlagRequired("namespace")
}

func init() {
	cliCtx = config.NewContext()

	// Add backup config flags to commands that need them
	esCmd := elasticsearch.Cmd(cliCtx)
	addBackupConfigFlags(esCmd)
	rootCmd.AddCommand(esCmd)

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
