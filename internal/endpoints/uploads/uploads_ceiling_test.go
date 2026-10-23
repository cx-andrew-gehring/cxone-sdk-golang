package uploads

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
)

func TestPutFileZeroTimeoutClientUsesCeiling(t *testing.T) {
	old := putCeiling
	putCeiling = 50 * time.Millisecond
	defer func() { putCeiling = old }()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	c := *srv.Client()
	c.Timeout = 0
	start := time.Now()
	err := PutFile(context.Background(), &c, srv.URL+"/o?X-Amz-Signature=SUPERSECRETSIG", bytes.NewReader([]byte("x")), 1)
	if time.Since(start) > 5*time.Second {
		t.Fatal("ceiling not applied")
	}
	var ce *cxerrors.CommunicationError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %T %v", err, err)
	}
	if strings.Contains(err.Error(), "SUPERSECRETSIG") || strings.Contains(ce.URL, "?") {
		t.Errorf("query leaked: %v / %s", err, ce.URL)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want deadline exceeded cause, got %v", err)
	}
}

func TestPutFileAuthWithTimeoutIgnoresCeiling(t *testing.T) {
	old := putCeiling
	putCeiling = time.Nanosecond
	defer func() { putCeiling = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	if err := PutFileWithTimeout(context.Background(), srv.Client(), srv.URL+"/o", bytes.NewReader([]byte("x")), 1, 5*time.Second); err != nil {
		t.Fatal(err)
	}
}
