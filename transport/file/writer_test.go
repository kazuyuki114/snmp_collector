package file_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/vpbank/snmp_collector/transport/file"
)

func newBuf(t *testing.T) (*bytes.Buffer, *file.WriterTransport) {
	t.Helper()
	var buf bytes.Buffer
	tr := file.New(file.Config{Writer: &buf}, nil)
	return &buf, tr
}

func TestSend_WritesDataAndNewline(t *testing.T) {
	buf, tr := newBuf(t)
	msg := []byte(`{"test":true}`)

	if err := tr.Send(msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := buf.String()
	if !strings.HasPrefix(got, `{"test":true}`) {
		t.Errorf("output = %q, want prefix %q", got, `{"test":true}`)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output should end with newline, got %q", got)
	}
}

func TestSend_MultipleMessages(t *testing.T) {
	buf, tr := newBuf(t)
	msgs := []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}

	for _, m := range msgs {
		if err := tr.Send([]byte(m)); err != nil {
			t.Fatalf("Send(%q): %v", m, err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
	for i, want := range msgs {
		if lines[i] != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestSend_CustomNewline(t *testing.T) {
	var buf bytes.Buffer
	tr := file.New(file.Config{Writer: &buf, Newline: "\r\n"}, nil)
	_ = tr.Send([]byte(`{"x":1}`))

	if !strings.HasSuffix(buf.String(), "\r\n") {
		t.Errorf("expected CRLF newline, got %q", buf.String())
	}
}

func TestSend_DefaultWriterIsStdout(t *testing.T) {
	// Constructing with zero Config must not panic (defaults to os.Stdout).
	tr := file.New(file.Config{}, nil)
	if tr == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestClose_ReturnsNil(t *testing.T) {
	_, tr := newBuf(t)
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSend_ConcurrentSafe(t *testing.T) {
	buf, tr := newBuf(t)
	const n = 100
	msg := []byte(`{"concurrent":true}`)

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = tr.Send(msg)
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != n {
		t.Errorf("expected %d lines, got %d", n, len(lines))
	}
}

func TestSend_NilLoggerDoesNotPanic(t *testing.T) {
	var buf bytes.Buffer
	tr := file.New(file.Config{Writer: &buf}, nil)
	if err := tr.Send([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("Send with nil logger: %v", err)
	}
}

func TestSend_ErrorOnClosedWriter(t *testing.T) {
	// Use a writer that always returns an error.
	tr := file.New(file.Config{Writer: &errWriter{}}, nil)
	err := tr.Send([]byte(`{"x":1}`))
	if err == nil {
		t.Error("expected error from failing writer, got nil")
	}
}

// errWriter always fails.
type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, &writeError{}
}

type writeError struct{}

func (e *writeError) Error() string { return "simulated write error" }

// Ensure WriterTransport satisfies the Transport interface at compile time.
var _ file.Transport = (*file.WriterTransport)(nil)
