package migration

import "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"

// Backend bundles the dependencies the migration workflow needs.
type Backend struct {
	Executor *transport.Executor
	BaseURL  string
}
