package users

import "github.com/cx-andrew-gehring/cxone-sdk-golang/internal/transport"

// Backend bundles the dependencies the user workflow needs to call the
// underlying endpoint package. IAM endpoints target the Keycloak admin
// realm, so this backend carries iamAdminURL instead of the API baseURL.
type Backend struct {
	Executor    *transport.Executor
	IAMAdminURL string
}
