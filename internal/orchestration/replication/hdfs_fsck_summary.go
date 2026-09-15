package replication

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	fsckReplicated = "replicated"
	fsckEC         = "ec"
)

func (p *fsckParser) summary(line string) error {
	if strings.Contains(line, "not validated") || strings.Contains(line, "Files currently being written:") ||
		strings.Contains(line, "Total open files size:") {
		return fmt.Errorf("fsck excluded open-file evidence from the audit")
	}
	switch line {
	case "Replicated Blocks:":
		if p.section != "" {
			return fmt.Errorf("duplicate fsck replication summary")
		}
		p.section = fsckReplicated
		return nil
	case "Erasure Coded Block Groups:":
		if p.section != fsckReplicated || !p.summaryFiles || !p.summaryBlocks {
			return fmt.Errorf("incomplete fsck replication summary")
		}
		p.section = fsckEC
		return nil
	}
	if strings.HasPrefix(line, "FSCK ended at ") {
		if p.section != fsckEC || !p.ecFiles || !p.ecBlocks || p.ended {
			return fmt.Errorf("incomplete fsck completion summary")
		}
		p.ended = true
		return nil
	}
	if strings.HasPrefix(line, "The filesystem under path ") {
		if !p.ended || line != "The filesystem under path '/' is "+p.status {
			return fmt.Errorf("missing or inconsistent fsck completion status")
		}
		p.done = true
		return nil
	}
	if p.ended {
		return fmt.Errorf("unexpected output after fsck summary")
	}
	if strings.HasPrefix(line, "Total files:") {
		return p.fileSummary(line)
	}
	if strings.HasPrefix(line, "Total blocks (validated):") {
		count, err := fsckSummaryCount(line)
		if err != nil || p.section != fsckReplicated || p.summaryBlocks || count != p.completed+p.open {
			return fmt.Errorf("fsck summary block count does not match audited blocks")
		}
		p.summaryBlocks = true
		return nil
	}
	if strings.HasPrefix(line, "Total block groups (validated):") {
		count, err := fsckSummaryCount(line)
		if err != nil || p.section != fsckEC || p.ecBlocks || count != 0 {
			return fmt.Errorf("erasure-coded block groups are not supported by the replication audit")
		}
		p.ecBlocks = true
		return nil
	}
	return p.summaryMetric(line)
}

func fsckSummaryCount(line string) (int64, error) {
	_, value, _ := strings.Cut(line, ":")
	match := fsckNumber.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, fmt.Errorf("invalid fsck summary counter")
	}
	count, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid fsck summary counter")
	}
	return count, nil
}

func (p *fsckParser) summaryMetric(line string) error {
	if strings.Trim(line, "* ") == "" {
		return nil
	}
	for _, prefix := range []string{"Missing blocks:", "Corrupt blocks:", "Missing replicas:", "Blocks queued for replication:",
		"Under-replicated blocks:", "Mis-replicated blocks:", "UNDER MIN REPL'D BLOCKS:", "CORRUPT FILES:", "MISSING BLOCKS:", "CORRUPT BLOCKS:"} {
		if strings.HasPrefix(line, prefix) {
			count, err := fsckSummaryCount(line)
			if err != nil {
				return err
			}
			if count > 0 {
				p.problem(fmt.Sprintf("%s %d", prefix, count))
			}
			return nil
		}
	}
	for _, prefix := range []string{"Number of data-nodes:", "Number of racks:", "Total dirs:", "Total symlinks:", "Total size:",
		"Minimally replicated blocks:", "Over-replicated blocks:", "Default replication factor:", "Average block replication:",
		"Minimally erasure-coded block groups:", "Over-erasure-coded block groups:", "Under-erasure-coded block groups:",
		"Unsatisfactory placement block groups:", "Average block group size:", "Missing block groups:", "Corrupt block groups:",
		"Missing internal blocks:", "DecommissionedReplicas:", "DecommissioningReplicas:", "EnteringMaintenanceReplicas:",
		"InMaintenanceReplicas:", "MINIMAL BLOCK REPLICATION:", "MISSING SIZE:", "CORRUPT SIZE:"} {
		if strings.HasPrefix(line, prefix) {
			return nil
		}
	}
	return fmt.Errorf("unsupported fsck summary record")
}

func (p *fsckParser) fileSummary(line string) error {
	count, err := fsckSummaryCount(line)
	if err != nil {
		return err
	}
	switch p.section {
	case fsckReplicated:
		if p.summaryFiles || count != p.files {
			return fmt.Errorf("fsck summary file count does not match audited files")
		}
		p.summaryFiles = true
	case fsckEC:
		if p.ecFiles || count != 0 {
			return fmt.Errorf("erasure-coded files are not supported by the replication audit")
		}
		p.ecFiles = true
	default:
		return fmt.Errorf("unexpected fsck file summary")
	}
	return nil
}
