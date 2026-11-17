package githubapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticatedMetadataAndNoRedirectCredentialLeak(t *testing.T) {
	var leaked atomic.Bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer foreign.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Error("missing authentication")
		}
		if r.URL.Path == "/repos/acme/app/releases/latest" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"tag_name":"v1.2.3"}`))
			return
		}
		http.Redirect(w, r, foreign.URL, http.StatusFound)
	}))
	defer upstream.Close()
	client := Client{BaseURL: upstream.URL, Token: "private-token"}
	response, err := client.Get(context.Background(), "/repos/acme/app/releases/latest")
	if err != nil || string(response.Body) != `{"tag_name":"v1.2.3"}` {
		t.Fatalf("response=%v err=%v", response, err)
	}
	if _, err = client.Get(context.Background(), "/repos/acme/app/releases/tags/v1.2.3"); err == nil {
		t.Fatal("redirect accepted")
	}
	if leaked.Load() {
		t.Fatal("credential-bearing request followed a redirect")
	}
}

func TestRateLimitsRetryButPermissionFailuresDoNot(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusForbidden, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				if count < 3 {
					w.Header().Set("Retry-After", "0")
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.WriteHeader(status)
					w.Write([]byte(`{"message":"rate limit exceeded"}`))
					return
				}
				w.Write([]byte(`{}`))
			}))
			defer server.Close()
			c := Client{BaseURL: server.URL, Sleep: func(context.Context, time.Duration) error { return nil }}
			if _, err := c.Get(context.Background(), "/repos/acme/app/releases/latest"); err != nil || count != 3 {
				t.Fatalf("count=%d err=%v", count, err)
			}
		})
	}
	for _, status := range []int{401, 403, 404} {
		count := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count++
			w.WriteHeader(status)
			w.Write([]byte(`{"message":"not permitted"}`))
		}))
		c := Client{BaseURL: s.URL}
		_, err := c.Get(context.Background(), "/repos/acme/app/releases/latest")
		s.Close()
		if err == nil || count != 1 {
			t.Fatalf("status=%d count=%d err=%v", status, count, err)
		}
	}
}

func TestQuotaResetWaitIsBoundedAndErrorIsSafe(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
		w.Write([]byte(`{"message":"private-token"}`))
	}))
	defer server.Close()
	client := Client{BaseURL: server.URL, Token: "private-token", MaxWait: time.Second}
	_, err := client.Get(context.Background(), "/repos/acme/app/releases/latest")
	var apiError *Error
	if !errors.As(err, &apiError) || apiError.Status != 429 || apiError.Kind != "rate-limit" || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if err.Error() == "private-token" {
		t.Fatal("token leaked")
	}
}
