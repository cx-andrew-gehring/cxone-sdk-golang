package retry

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetryAfter caps a parsed delta-seconds value so absurd values cannot
// overflow time.Duration.
const maxRetryAfter = 24 * time.Hour

// ParseRetryAfter parses an HTTP Retry-After header value (RFC 9110 §10.2.3):
// either delta-seconds ("120") or an HTTP-date. A date in the past yields 0.
// ok is false when the value is empty or malformed.
func ParseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if v[0] >= '0' && v[0] <= '9' {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || n > int64(maxRetryAfter/time.Second) {
			return 0, false
		}
		return time.Duration(n) * time.Second, true
	}
	t, err := http.ParseTime(v)
	if err != nil {
		return 0, false
	}
	d := t.Sub(now)
	if d < 0 {
		d = 0
	}
	return d, true
}
