package users_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cx-andrew-gehring/cxone-sdk-golang/models"
	"github.com/cx-andrew-gehring/cxone-sdk-golang/workflows/users"
)

func TestGetOrCreateByUsernameFindsExisting(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `[{"id":"u1","username":"alice","email":"alice@example.com"}]`)
			return
		}
		t.Error("unexpected POST — should not create")
	})
	u, err := users.GetOrCreateByUsername(context.Background(), b, &models.User{Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u1" {
		t.Errorf("expected existing user u1, got %q", u.ID)
	}
}

func TestGetOrCreateByUsernameCreatesNew(t *testing.T) {
	var hits int32
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		switch {
		case r.Method == http.MethodGet && n == 1:
			// List: no match
			_, _ = io.WriteString(w, `[]`)
		case r.Method == http.MethodPost:
			// Create: return 201 with Location header
			w.Header().Set("Location", "/users/u-new")
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "u-new"):
			// Get by ID (follow-up after create)
			_, _ = io.WriteString(w, `{"id":"u-new","username":"newuser","email":"new@example.com"}`)
		default:
			_, _ = io.WriteString(w, `{"id":"u-new","username":"newuser","email":"new@example.com"}`)
		}
	})
	u, err := users.GetOrCreateByUsername(context.Background(), b, &models.User{
		Username: "newuser",
		Email:    "new@example.com",
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u-new" {
		t.Errorf("expected u-new, got %q", u.ID)
	}
}

func TestGetOrCreateByEmailFindsExisting(t *testing.T) {
	b := newBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `[{"id":"u1","username":"alice","email":"alice@example.com"}]`)
			return
		}
		t.Error("unexpected POST")
	})
	u, err := users.GetOrCreateByEmail(context.Background(), b, &models.User{Email: "alice@example.com", Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != "u1" {
		t.Errorf("expected u1, got %q", u.ID)
	}
}
