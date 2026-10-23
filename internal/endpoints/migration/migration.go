// Package migration implements the Checkmarx One data-import (migration)
// API. These endpoints drive tenant-to-tenant data migrations:
//
//  1. Upload the encrypted archive via the [uploads] package.
//  2. Call [StartImport] with the uploaded filename and the encryption key
//     used to encrypt the archive.
//  3. Poll [GetImport] until Status is terminal (completed/partial/failed/blank).
//  4. Optionally fetch the import logs with [GetImportLogs].
//
// Part of the SDK's low-level endpoint layer (CLAUDE.md §8). The workflow
// layer (workflows/migration, not yet implemented) wraps these primitives
// with an inspector + polling waiter.
package migration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
)

// Path is the imports collection endpoint relative to the API host root.
const Path = "api/imports"

// StartImport begins a data import. fileName and encryptionKey are required;
// mappingFileName is optional (a previously-uploaded project-id mapping
// file). Returns the new migration id.
// POST /api/imports → 201.
func StartImport(ctx context.Context, e *transport.Executor, baseURL, fileName, mappingFileName, encryptionKey string) (string, error) {
	if fileName == "" {
		return "", &cxerrors.ConfigurationError{Field: "fileName", Reason: "is required"}
	}
	if encryptionKey == "" {
		return "", &cxerrors.ConfigurationError{Field: "encryptionKey", Reason: "is required"}
	}
	req := models.ImportStartRequest{
		FileName:                fileName,
		ProjectsMappingFileName: mappingFileName,
		EncryptionKey:           encryptionKey,
	}
	var resp models.ImportStartResponse
	if err := transport.DoJSON(ctx, e, http.MethodPost,
		transport.JoinURL(baseURL, Path),
		nil, req, []int{http.StatusCreated, http.StatusOK}, &resp); err != nil {
		return "", err
	}
	return resp.MigrationID, nil
}

// List returns every import visible to the caller.
// GET /api/imports → 200.
func List(ctx context.Context, e *transport.Executor, baseURL string) ([]models.DataImport, error) {
	var out []models.DataImport
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches a single import by migration id.
// GET /api/imports/{id} → 200.
func Get(ctx context.Context, e *transport.Executor, baseURL, importID string) (*models.DataImport, error) {
	if importID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "importID", Reason: "is required"}
	}
	var out models.DataImport
	if err := transport.DoJSON(ctx, e, http.MethodGet,
		transport.JoinURL(baseURL, Path+"/"+importID),
		nil, nil, []int{http.StatusOK}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLogs downloads the log bundle for a finished import. The platform
// returns a 302-style redirect via a Location header pointing at a signed
// object-store URL; this helper follows it with the client's HTTP client so
// connection pools are shared.
//
// Unlike most endpoints in this package, the redirect target lives on the
// object store and is fetched outside the SDK funnel (no bearer token,
// similar to uploads.PutFile).
func GetLogs(ctx context.Context, e *transport.Executor, httpClient *http.Client, baseURL, importID string) ([]byte, error) {
	if importID == "" {
		return nil, &cxerrors.ConfigurationError{Field: "importID", Reason: "is required"}
	}
	if httpClient == nil {
		return nil, &cxerrors.ConfigurationError{Field: "httpClient", Reason: "is required"}
	}
	// Fetch the redirect with the funnel (carries the bearer token).
	req := &transport.Request{
		Method: http.MethodGet,
		URL:    transport.JoinURL(baseURL, Path+"/"+importID+"/logs/download"),
	}
	resp, err := e.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16<<10))

	location := resp.Header.Get("Location")
	if location == "" {
		return nil, &cxerrors.ResponseError{
			Method: http.MethodGet, URL: req.URL, StatusCode: resp.StatusCode,
			CorrelationID: resp.CorrelationID,
			Reason:        "GetLogs: missing Location header",
		}
	}

	// Follow the redirect directly against the object store — no bearer.
	downloadReq, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, fmt.Errorf("build log-download request: %w", err)
	}
	downloadResp, err := httpClient.Do(downloadReq)
	if err != nil {
		return nil, &cxerrors.CommunicationError{
			Method: http.MethodGet, URL: location, Attempt: 1, Cause: err,
		}
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode < 200 || downloadResp.StatusCode >= 300 {
		return nil, &cxerrors.CommunicationError{
			Method: http.MethodGet, URL: location, StatusCode: downloadResp.StatusCode, Attempt: 1,
		}
	}

	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, io.LimitReader(downloadResp.Body, 64<<20)); err != nil {
		return nil, fmt.Errorf("read log body: %w", err)
	}
	return buf.Bytes(), nil
}

