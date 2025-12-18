package elasticsearch

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/stackvista/stackstate-backup-cli/cmd/cmdutils"
	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
	"github.com/stackvista/stackstate-backup-cli/internal/orchestration/portforward"
)

func configureCmd(globalFlags *config.CLIGlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Configure Elasticsearch snapshot repository and SLM policy",
		Long:  `Configure Elasticsearch snapshot repository and Snapshot Lifecycle Management (SLM) policy for automated backups.`,
		Run: func(_ *cobra.Command, _ []string) {
			cmdutils.Run(globalFlags, runConfigure, cmdutils.MinioIsRequired)
		},
	}
}

func runConfigure(appCtx *app.Context) error {
	// Validate required configuration
	if appCtx.Config.Elasticsearch.SnapshotRepository.AccessKey == "" || appCtx.Config.Elasticsearch.SnapshotRepository.SecretKey == "" {
		return fmt.Errorf("accessKey and secretKey are required in the secret configuration")
	}

	// Setup port-forward to Elasticsearch
	serviceName := appCtx.Config.Elasticsearch.Service.Name
	localPort := appCtx.Config.Elasticsearch.Service.LocalPortForwardPort
	remotePort := appCtx.Config.Elasticsearch.Service.Port

	pf, err := portforward.SetupPortForward(appCtx.K8sClient, appCtx.Namespace, serviceName, localPort, remotePort, appCtx.Logger)
	if err != nil {
		return err
	}
	defer close(pf.StopChan)

	// Configure snapshot repository
	repo := appCtx.Config.Elasticsearch.SnapshotRepository
	appCtx.Logger.Infof("Configuring snapshot repository '%s' (bucket: %s)...", repo.Name, repo.Bucket)

	err = appCtx.ESClient.ConfigureSnapshotRepository(
		repo.Name,
		repo.Bucket,
		repo.Endpoint,
		repo.BasePath,
		repo.AccessKey,
		repo.SecretKey,
	)
	if err != nil {
		return fmt.Errorf("failed to configure snapshot repository: %w", err)
	}

	appCtx.Logger.Successf("Snapshot repository configured successfully")

	// Configure SLM policy
	slm := appCtx.Config.Elasticsearch.SLM
	appCtx.Logger.Infof("Configuring SLM policy '%s'...", slm.Name)

	err = appCtx.ESClient.ConfigureSLMPolicy(
		slm.Name,
		slm.Schedule,
		slm.SnapshotTemplateName,
		slm.Repository,
		slm.Indices,
		slm.RetentionExpireAfter,
		slm.RetentionMinCount,
		slm.RetentionMaxCount,
	)
	if err != nil {
		return fmt.Errorf("failed to configure SLM policy: %w", err)
	}

	appCtx.Logger.Successf("SLM policy configured successfully")
	appCtx.Logger.Println()
	appCtx.Logger.Successf("Configuration completed successfully")

	return nil
}
