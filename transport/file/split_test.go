package file_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vpbank/snmp_collector/transport/file"
)

// ─────────────────────────────────────────────────────────────────────────────
// SplitWriterTransport tests
// ─────────────────────────────────────────────────────────────────────────────

func newSplitBufs(t *testing.T) (*bytes.Buffer, *bytes.Buffer, *file.SplitWriterTransport) {
	t.Helper()
	var metricBuf, trapBuf bytes.Buffer
	tr := file.NewSplit(file.SplitConfig{
		MetricWriter: &metricBuf,
		TrapWriter:   &trapBuf,
	}, nil)
	return &metricBuf, &trapBuf, tr
}

func TestSplit_MetricRouting(t *testing.T) {
	metricBuf, trapBuf, tr := newSplitBufs(t)

	msg := []byte(`{"timestamp":"2026-02-26T10:30:00Z","device":{"hostname":"r1"},"metrics":[]}`)
	if err := tr.Send(msg); err != nil {
		t.Fatalf("Send metric: %v", err)
	}

	if metricBuf.Len() == 0 {
		t.Error("expected metric data in metricBuf, got empty")
	}
	if trapBuf.Len() != 0 {
		t.Errorf("expected empty trapBuf, got %q", trapBuf.String())
	}
	if !strings.HasSuffix(metricBuf.String(), "\n") {
		t.Errorf("metric output should end with newline, got %q", metricBuf.String())
	}
}

func TestSplit_TrapRouting(t *testing.T) {
	metricBuf, trapBuf, tr := newSplitBufs(t)

	msg := []byte(`{"timestamp":"2026-02-26T10:31:00Z","device":{"hostname":"s1"},"trap_info":{"trap_oid":"1.3.6.1.6.3.1.1.5.3"},"varbinds":[]}`)
	if err := tr.Send(msg); err != nil {
		t.Fatalf("Send trap: %v", err)
	}

	if trapBuf.Len() == 0 {
		t.Error("expected trap data in trapBuf, got empty")
	}
	if metricBuf.Len() != 0 {
		t.Errorf("expected empty metricBuf, got %q", metricBuf.String())
	}
	if !strings.HasSuffix(trapBuf.String(), "\n") {
		t.Errorf("trap output should end with newline, got %q", trapBuf.String())
	}
}

func TestSplit_MixedMessages(t *testing.T) {
	metricBuf, trapBuf, tr := newSplitBufs(t)

	metric1 := []byte(`{"device":{"hostname":"r1"},"metrics":[{"oid":"1.2.3","name":"test","value":42}]}`)
	trap1 := []byte(`{"device":{"hostname":"s1"},"trap_info":{"trap_oid":"1.3.6.1.6.3.1.1.5.3"}}`)
	metric2 := []byte(`{"device":{"hostname":"r2"},"metrics":[{"oid":"1.2.4","name":"test2","value":99}]}`)
	trap2 := []byte(`{"device":{"hostname":"s2"},"trap_info":{"trap_oid":"1.3.6.1.6.3.1.1.5.4"}}`)

	for _, msg := range [][]byte{metric1, trap1, metric2, trap2} {
		if err := tr.Send(msg); err != nil {
			t.Fatalf("Send: %v", err)
		}
	}

	metricLines := strings.Split(strings.TrimRight(metricBuf.String(), "\n"), "\n")
	trapLines := strings.Split(strings.TrimRight(trapBuf.String(), "\n"), "\n")

	if len(metricLines) != 2 {
		t.Errorf("expected 2 metric lines, got %d: %q", len(metricLines), metricBuf.String())
	}
	if len(trapLines) != 2 {
		t.Errorf("expected 2 trap lines, got %d: %q", len(trapLines), trapBuf.String())
	}
}

func TestSplit_ConcurrentSafe(t *testing.T) {
	metricBuf, trapBuf, tr := newSplitBufs(t)
	const n = 100

	metricMsg := []byte(`{"metrics":[{"oid":"1.2.3","value":1}]}`)
	trapMsg := []byte(`{"trap_info":{"trap_oid":"1.3.6.1.6.3.1.1.5.3"}}`)

	var wg sync.WaitGroup
	wg.Add(2 * n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = tr.Send(metricMsg)
		}()
		go func() {
			defer wg.Done()
			_ = tr.Send(trapMsg)
		}()
	}
	wg.Wait()

	metricLines := strings.Split(strings.TrimRight(metricBuf.String(), "\n"), "\n")
	trapLines := strings.Split(strings.TrimRight(trapBuf.String(), "\n"), "\n")

	if len(metricLines) != n {
		t.Errorf("expected %d metric lines, got %d", n, len(metricLines))
	}
	if len(trapLines) != n {
		t.Errorf("expected %d trap lines, got %d", n, len(trapLines))
	}
}

func TestSplit_CustomNewline(t *testing.T) {
	var metricBuf, trapBuf bytes.Buffer
	tr := file.NewSplit(file.SplitConfig{
		MetricWriter: &metricBuf,
		TrapWriter:   &trapBuf,
		Newline:      "\r\n",
	}, nil)

	_ = tr.Send([]byte(`{"metrics":[]}`))
	_ = tr.Send([]byte(`{"trap_info":{}}`))

	if !strings.HasSuffix(metricBuf.String(), "\r\n") {
		t.Errorf("expected CRLF newline in metric output, got %q", metricBuf.String())
	}
	if !strings.HasSuffix(trapBuf.String(), "\r\n") {
		t.Errorf("expected CRLF newline in trap output, got %q", trapBuf.String())
	}
}

func TestSplit_DefaultWriters(t *testing.T) {
	// Zero-value SplitConfig should not panic.
	tr := file.NewSplit(file.SplitConfig{}, nil)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestSplit_CloseReturnsNil_ForBuffers(t *testing.T) {
	_, _, tr := newSplitBufs(t)
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSplit_ErrorOnFailingWriter(t *testing.T) {
	tr := file.NewSplit(file.SplitConfig{
		MetricWriter: &errWriter{},
		TrapWriter:   &errWriter{},
	}, nil)

	if err := tr.Send([]byte(`{"metrics":[]}`)); err == nil {
		t.Error("expected error from failing metric writer, got nil")
	}
	if err := tr.Send([]byte(`{"trap_info":{}}`)); err == nil {
		t.Error("expected error from failing trap writer, got nil")
	}
}

// Ensure SplitWriterTransport satisfies the Transport interface.
var _ file.Transport = (*file.SplitWriterTransport)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// RotatingFile tests
// ─────────────────────────────────────────────────────────────────────────────

func TestRotatingFile_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath: path,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile: %v", err)
	}
	defer rf.Close()

	data := []byte("hello world\n")
	n, err := rf.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned %d, want %d", n, len(data))
	}

	content, _ := os.ReadFile(path)
	if string(content) != "hello world\n" {
		t.Errorf("file content = %q, want %q", content, "hello world\n")
	}
}

func TestRotatingFile_RotatesOnSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath:   path,
		MaxBytes:   50,
		MaxBackups: 3,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile: %v", err)
	}
	defer rf.Close()

	// Write enough data to trigger rotation.
	msg := []byte("12345678901234567890123456\n") // 27 bytes each
	for i := 0; i < 4; i++ {
		if _, err := rf.Write(msg); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// Expect the active file and at least one backup.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("active file should exist: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("backup .1 should exist: %v", err)
	}
}

func TestRotatingFile_PrunesOldBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath:   path,
		MaxBytes:   20,
		MaxBackups: 2,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile: %v", err)
	}
	defer rf.Close()

	// Write enough to trigger multiple rotations.
	msg := []byte("12345678901234567890\n") // 21 bytes
	for i := 0; i < 5; i++ {
		if _, err := rf.Write(msg); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	// MaxBackups=2, so .1 and .2 should exist but .3 should not.
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("backup .1 should exist: %v", err)
	}
	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf("backup .2 should exist: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error("backup .3 should have been pruned")
	}
}

func TestRotatingFile_RequiresFilePath(t *testing.T) {
	_, err := file.NewRotatingFile(file.RotateConfig{}, nil)
	if err == nil {
		t.Error("expected error for empty FilePath, got nil")
	}
}

func TestRotatingFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "test.log")

	rf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath: path,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile: %v", err)
	}
	defer rf.Close()

	if _, err := rf.Write([]byte("ok\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SplitWriterTransport + RotatingFile integration
// ─────────────────────────────────────────────────────────────────────────────

func TestSplit_WithRotatingFiles(t *testing.T) {
	dir := t.TempDir()
	metricPath := filepath.Join(dir, "metrics.json")
	trapPath := filepath.Join(dir, "traps.json")

	mrf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath:   metricPath,
		MaxBytes:   500,
		MaxBackups: 2,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile (metrics): %v", err)
	}

	trf, err := file.NewRotatingFile(file.RotateConfig{
		FilePath:   trapPath,
		MaxBytes:   500,
		MaxBackups: 2,
	}, nil)
	if err != nil {
		t.Fatalf("NewRotatingFile (traps): %v", err)
	}

	tr := file.NewSplit(file.SplitConfig{
		MetricWriter: mrf,
		TrapWriter:   trf,
	}, nil)

	// Send a mix of metrics and traps.
	for i := 0; i < 20; i++ {
		_ = tr.Send([]byte(`{"device":{"hostname":"r1"},"metrics":[{"oid":"1.2.3","value":42}]}`))
		_ = tr.Send([]byte(`{"device":{"hostname":"s1"},"trap_info":{"trap_oid":"1.3.6.1.6.3.1.1.5.3"}}`))
	}

	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify files exist.
	if _, err := os.Stat(metricPath); err != nil {
		t.Errorf("metric file should exist: %v", err)
	}
	if _, err := os.Stat(trapPath); err != nil {
		t.Errorf("trap file should exist: %v", err)
	}

	// Verify content was routed correctly.
	metricData, _ := os.ReadFile(metricPath)
	trapData, _ := os.ReadFile(trapPath)

	if bytes.Contains(metricData, []byte(`"trap_info"`)) {
		t.Error("metric file should not contain trap data")
	}
	if bytes.Contains(trapData, []byte(`"metrics"`)) && !bytes.Contains(trapData, []byte(`"trap_info"`)) {
		t.Error("trap file should only contain trap data")
	}
}
