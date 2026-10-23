package cxone_test

import (
	"strings"
	"testing"

	"github.com/checkmarx-open-labs/cxone-sdk-golang"
)

func TestIsAPIOrigin(t *testing.T) {
	r, err := cxone.NewCustomRegion("iam.example.test", "api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	c, err := cxone.NewClient().Region(r).Tenant("t").AgentName("a").APIKey("k").Build()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for in, want := range map[string]bool{
		"https://api.example.test/storage/uploads/x":      true,
		"https://API.example.test:443/storage/x":          true,
		"https://api.example.test:8443/storage/x":         false,
		"http://api.example.test/storage/x":               false,
		"https://bucket.s3.amazonaws.com/x":               false,
		"https://user@api.example.test/x":                 false,
		"https://api.example.test./x":                     false,
		"":                                                false,
		strings.Repeat("%", 3):                            false,
		"https://iam.example.test/auth/realms/t/whatever": false,
	} {
		if got := c.IsAPIOrigin(in); got != want {
			t.Errorf("IsAPIOrigin(%q) = %v want %v", in, got, want)
		}
	}
}
