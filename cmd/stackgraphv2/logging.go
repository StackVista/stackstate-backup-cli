package stackgraphv2

import (
	"strings"

	"github.com/stackvista/stackstate-backup-cli/internal/foundation/logger"
)

func logAfterJobResult(log *logger.Logger, jobName string, success bool) {
	switch {
	case strings.HasPrefix(jobName, restoreNameTemplate):
		if success {
			log.Println()
			log.Infof("Job '%s' has successfully restored the live data. The system is running again.", jobName)
			log.Infof("Now run `sts-backup stackgraph-v2 backfill` to load the historic data.")
		} else {
			log.Println()
			log.Infof("Job '%s' has failed to restore the live data. The system was brought back up but the data was not restored.", jobName)
			log.Infof("Rerun your `sts-backup stackgraph-v2 restore ...` command toretry restoring data.")
		}
	case strings.HasPrefix(jobName, backfillNameTemplate):
		if success {
			log.Println()
			log.Infof("Job '%s' has successfully backfilled the historic data. The restore is complete.", jobName)
		} else {
			log.Println()
			log.Infof("Job '%s' has failed to backfill all historic data.", jobName)
			log.Infof("Run `sts-backup stackgraph-v2 backfill` to retry restoring old data, or")
			log.Infof("run `sts-backup stackgraph-v2 abort` to leave the restored data as-is and continue with")
			log.Infof("the data currently in the system.")
		}
	case strings.HasPrefix(jobName, abortNameTemplate):
		if success {
			log.Println()
			log.Infof("Job '%s' has successfully aborted. Historic data is incomplete but the system is functional.", jobName)
		} else {
			log.Println()
			log.Infof("Job '%s' has failed to abort the restore.", jobName)
			log.Infof("Rerun `sts-backup stackgraph-v2 abort` again to try again.")
		}
	}
}
