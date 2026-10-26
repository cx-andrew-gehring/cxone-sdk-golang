package cxone

import "github.com/cx-andrew-gehring/cxone-sdk-golang/cxerrors"

// The SDK's typed error catalog. These are aliases of the canonical types
// declared in the cxerrors subpackage so callers can use errors.As against
// the cxone.* names without an extra import:
//
//	var commErr *cxone.CommunicationError
//	if errors.As(err, &commErr) {
//	    log.Printf("status=%d attempt=%d", commErr.StatusCode, commErr.Attempt)
//	}
//
// See cxerrors for full field documentation.
type (
	EndpointError      = cxerrors.EndpointError
	AuthError          = cxerrors.AuthError
	CommunicationError = cxerrors.CommunicationError
	ResponseError      = cxerrors.ResponseError
	ScanError          = cxerrors.ScanError
	ConfigurationError = cxerrors.ConfigurationError
	ReportError        = cxerrors.ReportError
	ConflictError      = cxerrors.ConflictError
	InvalidLLMError    = cxerrors.InvalidLLMError
)
