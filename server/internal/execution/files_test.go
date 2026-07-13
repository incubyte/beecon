// White-box (package execution) tests for files.go's unexported
// countingReader: UploadFile (facade.go, AC3) relies on it to measure a
// file's true size as it streams to storage, without ever buffering the
// whole upload in memory first to find out.
package execution

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCountingReader_CountsAllBytesReadAcrossMultipleReads(t *testing.T) {
	content := strings.Repeat("a", 5000) // bigger than one io.ReadAll internal buffer, forces multiple Read calls
	counted := &countingReader{reader: strings.NewReader(content)}

	read, err := io.ReadAll(counted)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(read) != len(content) {
		t.Fatalf("read %d bytes, want %d", len(read), len(content))
	}
	if counted.count != int64(len(content)) {
		t.Errorf("count = %d, want %d", counted.count, len(content))
	}
}

// TestCountingReader_CountsOnlyUpToALimitReaderCap mirrors how UploadFile
// actually uses countingReader (facade.go): wrapped around
// io.LimitReader(content, max+1) so an oversized upload's true size is
// capped, not silently truncated to the limit — count must reflect exactly
// what LimitReader allowed through, not the full underlying source.
func TestCountingReader_CountsOnlyUpToALimitReaderCap(t *testing.T) {
	source := strings.NewReader(strings.Repeat("b", 100))
	const cap = 10
	counted := &countingReader{reader: io.LimitReader(source, cap)}

	if _, err := io.ReadAll(counted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if counted.count != cap {
		t.Errorf("count = %d, want %d (the LimitReader cap, not the source's full length)", counted.count, cap)
	}
}

// TestCountingReader_CountReflectsBytesReadBeforeAnUnderlyingError proves
// count is accurate even when the wrapped reader fails partway through — a
// caller inspecting count after an error still sees exactly how much made it
// through before the failure.
func TestCountingReader_CountReflectsBytesReadBeforeAnUnderlyingError(t *testing.T) {
	failAfter := &failingReaderAfterNBytes{remaining: []byte("first-chunk"), failErr: errors.New("simulated read failure")}
	counted := &countingReader{reader: failAfter}

	_, err := io.ReadAll(counted)

	if err == nil {
		t.Fatal("expected the underlying error to propagate, got nil")
	}
	if !errors.Is(err, failAfter.failErr) {
		t.Errorf("error = %v, want it to wrap %v", err, failAfter.failErr)
	}
	if counted.count != int64(len("first-chunk")) {
		t.Errorf("count = %d, want %d (the bytes successfully read before the error)", counted.count, len("first-chunk"))
	}
}

// failingReaderAfterNBytes returns remaining once, then fails on every
// subsequent call — a hand-written io.Reader test double, since bytes.Reader
// alone cannot script a mid-stream failure.
type failingReaderAfterNBytes struct {
	remaining []byte
	served    bool
	failErr   error
}

func (r *failingReaderAfterNBytes) Read(p []byte) (int, error) {
	if r.served {
		return 0, r.failErr
	}
	r.served = true
	n := copy(p, r.remaining)
	return n, nil
}
