package scans_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/scans"
)

func TestWaitUntilCompleteHappyPath(t *testing.T) {
	var hits int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		switch n {
		case 1, 2:
			_, _ = io.WriteString(w, `{"id":"s1","status":"Running"}`)
		default:
			_, _ = io.WriteString(w, `{"id":"s1","status":"Completed"}`)
		}
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	final, err := scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Successful() {
		t.Errorf("expected successful, got %q", final.Status())
	}
	if got := atomic.LoadInt32(&hits); got < 3 {
		t.Errorf("expected at least 3 polls, got %d", got)
	}
}

func TestWaitUntilCompleteTerminalFailureReturnsScanError(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"s1","status":"Failed","statusDetails":[{"name":"sast","status":"Failed","details":"engine crash"}]}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	var serr *cxerrors.ScanError
	if !errors.As(err, &serr) {
		t.Fatalf("expected *ScanError, got %T: %v", err, err)
	}
	if serr.ScanID != "s1" {
		t.Errorf("scanID = %q", serr.ScanID)
	}
}

func TestWaitUntilCompleteSuppressFailure(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"s1","status":"Failed"}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	final, err := scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval:    5 * time.Millisecond,
		SuppressFailure: true,
	})
	if err != nil {
		t.Fatalf("expected nil error with SuppressFailure, got %v", err)
	}
	if !final.Failed() {
		t.Errorf("inspector should still report Failed")
	}
}

func TestWaitUntilCompleteContextCancellationReturnsLastInspector(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"s1","status":"Running"}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	final, err := scans.WaitUntilComplete(ctx, insp, scans.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected ctx.Canceled, got %v", err)
	}
	if final == nil || !final.Executing() {
		t.Errorf("expected last inspector to still be Executing")
	}
}

func TestWaitUntilCompleteMaxDurationDeadline(t *testing.T) {
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"s1","status":"Running"}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval: 10 * time.Millisecond,
		MaxDuration:  50 * time.Millisecond,
	})
	elapsed := time.Since(start)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("waiter ran for %v, expected ~50ms", elapsed)
	}
}

func TestWaitUntilCompletePartialBecomesSuccessful(t *testing.T) {
	var hits int32
	c := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		// First poll: Partial with one engine still running.
		// Second poll: Partial with all engines completed → Successful.
		if n == 1 {
			body := fmt.Sprintf(`{"id":"s1","status":"Partial","engines":["sast","sca"],"statusDetails":[{"name":"sast","status":"Completed"},{"name":"sca","status":"Running"}]}`)
			_, _ = io.WriteString(w, body)
			return
		}
		_, _ = io.WriteString(w, `{"id":"s1","status":"Partial","engines":["sast","sca"],"statusDetails":[{"name":"sast","status":"Completed"},{"name":"sca","status":"Completed"}]}`)
	})
	insp, err := scans.FromScanID(context.Background(), c, "s1")
	if err != nil {
		t.Fatal(err)
	}
	final, err := scans.WaitUntilComplete(context.Background(), insp, scans.WaitOptions{
		PollInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Successful() {
		t.Errorf("expected partial-then-complete to be Successful, status=%q msg=%q",
			final.Status(), final.StateMessage())
	}
}
