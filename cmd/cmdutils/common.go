package cmdutils

import (
	"fmt"
	"os"

	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

const (
	MinioIsRequired    bool = true
	MinioIsNotRequired bool = false
)

func Run(globalFlags *config.CLIGlobalFlags, runFunc func(ctx *app.Context) error, minioRequired bool) {
	appCtx, err := app.NewContext(globalFlags)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if minioRequired && !appCtx.Config.Minio.Enabled {
		appCtx.Logger.Errorf("commands that interact with Minio require SUSE Observability to be deployed with .Values.global.backup.enabled=true")
		os.Exit(1)
	}
	if err := runFunc(appCtx); err != nil {
		appCtx.Logger.Errorf(err.Error())
		os.Exit(1)
	}
}
