package replication

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxFsckLine        = 64 << 10
	maxFsckDiagnostics = 8
	maxDiagnosticPath  = 160
)

var (
	fsckFilePattern  = regexp.MustCompile(`^(/.+) [0-9]+ bytes, replicated: replication=([0-9]+), ([0-9]+) block\(s\)(:|, OPENFORWRITE:)(.*)$`)
	fsckBlockPattern = regexp.MustCompile(`^([0-9]+)\. (\S+:blk_-?[0-9]+_[0-9]+) len=[0-9]+(?: (Live_repl|Expected_repl)=([0-9]+)| (MISSING!))(.*)$`)
	fsckNumber       = regexp.MustCompile(`^([0-9]+)(?:\s|$)`)
)

type fsckFile struct {
	path              string
	target, declared  int64
	seen              int64
	open, expecting   bool
	underConstruction bool
}

type fsckParser struct {
	pending                          []byte
	err                              error
	lines                            int64
	started, statusSeen, ended, done bool
	status                           string
	section                          string
	file                             *fsckFile
	files, completed, open           int64
	summaryFiles, summaryBlocks      bool
	ecFiles, ecBlocks                bool
	problems                         int64
	messages                         []string
}

func (p *fsckParser) Write(data []byte) (int, error) {
	size := len(data)
	for len(data) > 0 && p.err == nil {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			end = len(data)
		}
		if len(p.pending)+end > maxFsckLine {
			if len(p.pending) < maxDiagnosticPath {
				p.pending = append(p.pending, data[:min(end, maxDiagnosticPath-len(p.pending)+1)]...)
			}
			p.lines++
			p.err = p.lineError(fmt.Errorf("fsck line exceeds the supported size"))
			break
		}
		p.pending = append(p.pending, data[:end]...)
		if end == len(data) {
			break
		}
		p.consumeLine()
		data = data[end+1:]
	}
	// Keep draining after a parse error; only the bounded diagnostic is retained.
	return size, nil
}

func (p *fsckParser) consumeLine() {
	p.lines++
	if err := p.line(strings.TrimSpace(string(p.pending))); err != nil {
		p.err = p.lineError(err)
	}
	p.pending = p.pending[:0]
}

func (p *fsckParser) lineError(err error) error {
	return fmt.Errorf("fsck line %d: %w; record %q", p.lines, err, diagnosticPath(string(p.pending)))
}

func (p *fsckParser) result() Result {
	if p.err == nil && len(p.pending) != 0 {
		p.consumeLine()
		p.pending = nil
	}
	if p.err != nil {
		return result("hdfs", Unknown, p.err.Error())
	}
	if !p.done || !p.summaryFiles || !p.summaryBlocks || !p.ecFiles || !p.ecBlocks {
		return result("hdfs", Unknown, "incomplete fsck report; block replication could not be verified")
	}
	if p.problems > 0 {
		return Result{Component: "hdfs", Status: Degraded,
			Messages: append([]string{fmt.Sprintf("HDFS block audit found %d replication problems", p.problems)}, p.messages...)}
	}
	report := result("hdfs", Healthy, fmt.Sprintf("HDFS audit: %d files and %d completed block entries meet replication targets of at least two (including snapshot references)", p.files, p.completed))
	if p.open > 0 {
		report.Messages = append(report.Messages, fmt.Sprintf("%d under-construction blocks have expected pipeline membership meeting their targets; persistence of their latest writes is not verified", p.open))
	}
	return report
}

func (p *fsckParser) line(line string) error {
	if line == "" {
		return nil
	}
	if p.done {
		return fmt.Errorf("unexpected output after fsck completion")
	}
	if !p.started {
		if !strings.HasPrefix(line, "FSCK started by ") || !strings.Contains(line, " for path / at ") {
			return fmt.Errorf("missing fsck start marker for the filesystem root")
		}
		p.started = true
		return nil
	}
	if p.statusSeen {
		return p.summary(line)
	}
	if line == "Status: HEALTHY" || line == "Status: CORRUPT" {
		if err := p.finishFile(); err != nil {
			return err
		}
		p.statusSeen, p.status = true, strings.TrimPrefix(line, "Status: ")
		if p.status == "CORRUPT" {
			p.problem("fsck reports missing or corrupt data")
		}
		return nil
	}
	if match := fsckFilePattern.FindStringSubmatch(line); match != nil {
		return p.startFile(match)
	}
	if strings.HasPrefix(line, "/") && strings.HasSuffix(line, " <dir>") {
		return p.finishFile()
	}
	if line == "Under Construction Block:" {
		if p.file == nil || !p.file.open || p.file.expecting || p.file.underConstruction {
			return fmt.Errorf("unexpected under-construction block marker")
		}
		p.file.expecting = true
		return nil
	}
	if match := fsckBlockPattern.FindStringSubmatch(line); match != nil {
		return p.block(match)
	}
	if p.file != nil && fsckWarning(line) {
		p.problem("fsck reports a file replication or placement problem")
		return nil
	}
	return fmt.Errorf("unsupported fsck file or block record")
}

func (p *fsckParser) finishFile() error {
	if p.file != nil && (p.file.seen != p.file.declared || p.file.expecting) {
		return fmt.Errorf("fsck file block count does not match its block records")
	}
	p.file = nil
	return nil
}

func (p *fsckParser) startFile(match []string) error {
	if err := p.finishFile(); err != nil {
		return err
	}
	target, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil || target < 1 {
		return fmt.Errorf("invalid fsck replication target")
	}
	blocks, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid fsck file block count")
	}
	p.file = &fsckFile{path: match[1], target: target, declared: blocks, open: match[4] == ", OPENFORWRITE:"}
	p.files++
	if target < minReplicas {
		p.problem(fmt.Sprintf("file %q has replication target %d", diagnosticPath(match[1]), target))
	}
	tail := strings.TrimSpace(match[5])
	if tail != "" && tail != "OK" {
		if !fsckWarning(tail) {
			return fmt.Errorf("unsupported fsck file status")
		}
		p.problem("fsck reports a file replication or placement problem")
	}
	return nil
}

func (p *fsckParser) block(match []string) error {
	index, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || p.file == nil || p.file.seen != index || p.file.seen >= p.file.declared || p.file.underConstruction {
		return fmt.Errorf("unexpected or duplicate fsck block record")
	}
	p.file.seen++
	if match[5] == "MISSING!" {
		if p.file.expecting {
			return fmt.Errorf("missing pipeline evidence for under-construction block")
		}
		p.completed++
		p.problem(fmt.Sprintf("block %s is missing", match[2]))
		return nil
	}
	replicas, err := strconv.ParseInt(match[4], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid fsck replica count")
	}
	expected := match[3] == "Expected_repl"
	if expected != p.file.expecting {
		return fmt.Errorf("fsck live replicas and expected pipeline evidence are inconsistent")
	}
	if expected {
		p.open++
		p.file.expecting, p.file.underConstruction = false, true
	} else {
		p.completed++
	}
	if replicas < p.file.target {
		p.problem(fmt.Sprintf("block %s in %q has %d/%d %s", match[2], diagnosticPath(p.file.path), replicas, p.file.target, match[3]))
	}
	if strings.TrimSpace(match[6]) != "" {
		return fmt.Errorf("unsupported fsck block details")
	}
	return nil
}

func fsckWarning(line string) bool {
	for _, marker := range []string{"Under replicated ", "Replica placement policy is violated", "CORRUPT", "MISSING"} {
		if strings.HasPrefix(line, marker) || strings.Contains(line, ": "+marker) {
			return true
		}
	}
	return false
}

func diagnosticPath(path string) string {
	if len(path) > maxDiagnosticPath {
		return path[:maxDiagnosticPath] + "..."
	}
	return path
}

func (p *fsckParser) problem(message string) {
	p.problems++
	if len(p.messages) < maxFsckDiagnostics {
		p.messages = append(p.messages, message)
	}
}
