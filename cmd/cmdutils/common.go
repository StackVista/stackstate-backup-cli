package cmdutils

import (
	"fmt"
	"os"

	"github.com/stackvista/stackstate-backup-cli/internal/app"
	"github.com/stackvista/stackstate-backup-cli/internal/foundation/config"
)

const (
	StorageIsRequired    bool = true
	StorageIsNotRequired bool = false
)

func Run(globalFlags *config.CLIGlobalFlags, runFunc func(ctx *app.Context) error, storageRequired bool) {
	appCtx, err := app.NewContext(globalFlags)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
		os.Exit(1)
	}
	if storageRequired && !appCtx.Config.StorageEnabled() {
		appCtx.Logger.Errorf("commands that interact with S3-compatible storage require SUSE Observability to be deployed with .Values.global.backup.enabled=true")
		os.Exit(1)
	}
	if err := runFunc(appCtx); err != nil {
		appCtx.Logger.Errorf(err.Error())
		os.Exit(1)
	}
}
