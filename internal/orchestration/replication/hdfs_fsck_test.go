package replication

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fsckStart = "FSCK started by checker (auth:SIMPLE) from /127.0.0.1 for path / at Tue Sep 15 08:00:00 UTC 2026\n/ <dir>\n"

func fsckSummary(files, blocks int) string {
	return fmt.Sprintf(`
Status: HEALTHY
 Number of data-nodes: 3
 Total dirs: 1
 Total symlinks: 0
Replicated Blocks:
 Total files: %d
 Total blocks (validated): %d
 Missing blocks: 0
 Corrupt blocks: 0
Erasure Coded Block Groups:
 Total files: 0
 Total block groups (validated): 0
FSCK ended at Tue Sep 15 08:00:01 UTC 2026 in 100 milliseconds
The filesystem under path '/' is HEALTHY`, files, blocks)
}

func fsckFixture() string {
	return fsckStart + `/hbase/table/region/file 100 bytes, replicated: replication=2, 1 block(s):  OK
0. BP-test:blk_1_1 len=100 Live_repl=2
/hbase/WALs/active 100 bytes, replicated: replication=2, 2 block(s), OPENFORWRITE:  OK
0. BP-test:blk_2_1 len=80 Live_repl=2
Under Construction Block:
1. BP-test:blk_3_1 len=20 Expected_repl=2
` + fsckSummary(2, 3)
}

func TestFsckEvidence(t *testing.T) {
	tests := []struct {
		name, status string
		change       func(string) string
	}{
		{"completed blocks and active WAL", Healthy, func(s string) string { return s }},
		{"replication one despite healthy footer", Degraded, func(s string) string {
			return strings.Replace(s, "replication=2", "replication=1", 1)
		}},
		{"live count below target", Degraded, func(s string) string { return strings.Replace(s, "Live_repl=2", "Live_repl=1", 1) }},
		{"target three still needs three copies", Degraded, func(s string) string { return strings.Replace(s, "replication=2", "replication=3", 1) }},
		{"short active pipeline", Degraded, func(s string) string { return strings.ReplaceAll(s, "Expected_repl=2", "Expected_repl=1") }},
		{"missing block", Degraded, func(s string) string {
			s = strings.Replace(s, "Live_repl=2", "MISSING!", 1)
			return strings.ReplaceAll(s, "HEALTHY", "CORRUPT")
		}},
		{"snapshot references", Healthy, func(s string) string {
			return strings.Replace(s, "/hbase/table/region/file", "/hbase/.snapshot/backup/table/region/file", 1)
		}},
		{"missing start", Unknown, func(s string) string { return strings.TrimPrefix(s, fsckStart) }},
		{"missing footer", Unknown, func(s string) string {
			return strings.Split(s, "The filesystem under path")[0]
		}},
		{"missing summary", Unknown, func(s string) string {
			return strings.Split(s, "Status:")[0] + "The filesystem under path '/' is HEALTHY"
		}},
		{"missing block detail", Unknown, func(s string) string {
			return strings.Replace(s, "0. BP-test:blk_1_1 len=100 Live_repl=2\n", "", 1)
		}},
		{"duplicate block detail", Unknown, func(s string) string {
			return strings.Replace(s, "0. BP-test:blk_1_1 len=100 Live_repl=2\n",
				"0. BP-test:blk_1_1 len=100 Live_repl=2\n0. BP-test:blk_1_1 len=100 Live_repl=2\n", 1)
		}},
		{"file total mismatch", Unknown, func(s string) string { return strings.Replace(s, "Total files: 2", "Total files: 3", 1) }},
		{"block total mismatch", Unknown, func(s string) string {
			return strings.Replace(s, "Total blocks (validated): 3", "Total blocks (validated): 4", 1)
		}},
		{"open-file evidence excluded", Unknown, func(s string) string {
			return strings.Replace(s, "Total blocks (validated): 3", "Total blocks (validated): 3 (Total open file blocks (not validated): 1)", 1)
		}},
		{"missing pipeline marker", Unknown, func(s string) string { return strings.ReplaceAll(s, "Under Construction Block:\n", "") }},
		{"expected count is not a live count", Unknown, func(s string) string { return strings.Replace(s, "Live_repl=2", "Expected_repl=2", 1) }},
		{"unexpected output", Unknown, func(s string) string { return s + "\npermission denied" }},
		{"integer overflow", Unknown, func(s string) string {
			return strings.Replace(s, "replication=2", "replication=99999999999999999999999", 1)
		}},
		{"erasure coded files", Unknown, func(s string) string {
			return strings.Replace(s, "Erasure Coded Block Groups:\n Total files: 0", "Erasure Coded Block Groups:\n Total files: 1", 1)
		}},
		{"unsupported layout", Unknown, func(s string) string {
			return strings.Replace(s, "replicated: replication=2", "erasure-coded: policy=RS-6-3", 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := &fsckParser{}
			data := test.change(fsckFixture())
			for len(data) > 0 {
				size := min(len(data), 17)
				_, err := io.WriteString(parser, data[:size])
				require.NoError(t, err)
				data = data[size:]
			}
			report := parser.result()
			assert.Equal(t, test.status, report.Status, report)
			if test.name == "completed blocks and active WAL" {
				assert.Contains(t, report.Messages[0], "2 completed block entries")
				assert.Contains(t, report.Messages[1], "1 under-construction blocks")
				assert.Contains(t, report.Messages[1], "latest writes is not verified")
			}
		})
	}
}

func TestFsckEmptyFilesystem(t *testing.T) {
	parser := &fsckParser{}
	_, err := io.WriteString(parser, fsckStart+fsckSummary(0, 0))
	require.NoError(t, err)
	assert.Equal(t, Healthy, parser.result().Status)
}

func TestFsckStreamingAndBoundedDiagnostics(t *testing.T) {
	parser := &fsckParser{}
	blocks := 200000
	written, err := fmt.Fprintf(parser, "%s/hbase/data 200000 bytes, replicated: replication=2, %d block(s): OK\n", fsckStart, blocks)
	require.NoError(t, err)
	for n := range blocks {
		size, err := fmt.Fprintf(parser, "%d. BP-test:blk_%d_1 len=1 Live_repl=1\n", n, n)
		require.NoError(t, err)
		written += size
	}
	_, err = io.WriteString(parser, fsckSummary(1, blocks))
	require.NoError(t, err)
	report := parser.result()
	assert.Greater(t, written, 8<<20)
	assert.Equal(t, Degraded, report.Status)
	assert.Equal(t, maxFsckDiagnostics+1, len(report.Messages))
	assert.LessOrEqual(t, cap(parser.pending), maxFsckLine)
}

func TestFsckRejectsOversizedLine(t *testing.T) {
	parser := &fsckParser{}
	_, err := io.WriteString(parser, strings.Repeat("x", maxFsckLine+1))
	require.NoError(t, err)
	assert.Equal(t, Unknown, parser.result().Status)
	assert.LessOrEqual(t, len(parser.pending), maxFsckLine)
}
