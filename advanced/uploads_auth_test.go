package advanced_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang"
)

// Storage fronted by the API host: POST /api/uploads returns a URL on the
// same origin and the PUT must carry the client's bearer token.
func TestAdvancedUploadFromReaderSameHostStorageSendsBearer(t *testing.T) {
	var putAuth, putCT string
	var putBody []byte
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/realms/acme/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"access_token":"tok-1","token_type":"Bearer","expires_in":300}`)
	})
	mux.HandleFunc("/api/uploads", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"url":"`+srvURL+`/storage/uploads.x/tenant/obj"}`)
	})
	mux.HandleFunc("/storage/", func(w http.ResponseWriter, r *http.Request) {
		putAuth = r.Header.Get("Authorization")
		putCT = r.Header.Get("Content-Type")
		putBody, _ = io.ReadAll(r.Body)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	host := strings.TrimPrefix(srv.URL, "http://")
	region, err := cxone.NewCustomRegion(host, host)
	if err != nil {
		t.Fatal(err)
	}
	if region, err = region.WithScheme("http"); err != nil {
		t.Fatal(err)
	}
	c, err := cxone.NewClient().Region(region).Tenant("acme").AgentName("t").
		APIKey("dummy").HTTPClient(srv.Client()).Build()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	body := []byte("PK\x03\x04zip")
	if _, err := c.Advanced().Uploads().UploadFromReader(context.Background(), bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if putAuth != "Bearer tok-1" {
		t.Errorf("PUT Authorization = %q", putAuth)
	}
	if putCT != "application/zip" || !bytes.Equal(putBody, body) {
		t.Errorf("ct=%q body=%q", putCT, putBody)
	}
}
