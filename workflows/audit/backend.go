package audit

import "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"

// Backend bundles the dependencies the audit workflow needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}
