package cmdutils

import (
	"errors"
	"fmt"
	"io"
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
		exitWithError(err, os.Stderr)
	}
	if minioRequired && !appCtx.Config.Minio.Enabled {
		exitWithError(errors.New("commands that interact with Minio require SUSE Observability to be deployed with .Values.global.backup.enabled=true"), os.Stderr)
	}
	if err := runFunc(appCtx); err != nil {
		exitWithError(err, os.Stderr)
	}
}

// ExitWithError prints an error message to the writer and exits with status code 1.
// This is a helper function to avoid repeating error handling code in commands.
func exitWithError(err error, w io.Writer) {
	_, _ = fmt.Fprintf(w, "error: %v\n", err)
	os.Exit(1)
}
