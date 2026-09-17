package storelookup_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/storelookup"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func TestDefaultLooksUpOfficialVersionAnonymously(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/zh/v3/app_cloud.lazycat.example.json" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != "" {
			t.Fatalf("anonymous lookup carried credentials: %#v", request.Header)
		}
		_, _ = response.Write([]byte(`{"package":"cloud.lazycat.example","version":{"name":"1.2.3","package":"cloud.lazycat.example"}}`))
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	serverURL, _ := url.Parse(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "session", Value: "must-not-send"}})
	httpClient := *server.Client()
	httpClient.Jar = jar

	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StoreOfficial, PackageID: "cloud.lazycat.example", BaseURL: server.URL, HTTPClient: &httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OnlineVersion != "1.2.3" {
		t.Fatalf("result=%#v", result)
	}
}

func TestDefaultLooksUpPrivateVersionWithGroupCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/packages/community.lazycat.example/latest-version" {
			t.Fatalf("path=%q", request.URL.Path)
		}
		if got := request.Header.Get("X-Group-Codes"); got != "ABC123,LATE23" {
			t.Fatalf("X-Group-Codes=%q", got)
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
			t.Fatalf("private lookup carried account credentials: %#v", request.Header)
		}
		_, _ = response.Write([]byte(`{"packageId":"community.lazycat.example","latestVersion":{"version":"2.0.0","createdAt":"2026-07-11T00:00:00Z"}}`))
	}))
	defer server.Close()

	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: server.URL,
		GroupCodes: []string{" abc123 ", "LATE23", "ABC123"}, HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OnlineVersion != "2.0.0" {
		t.Fatalf("result=%#v", result)
	}
}

func TestDefaultRetriesTransientPrivateLatestVersionResponses(t *testing.T) {
	for _, status := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				attempts++
				if attempts == 1 {
					http.Error(response, "transient upstream failure", status)
					return
				}
				_, _ = response.Write([]byte(`{"packageId":"community.lazycat.example","latestVersion":{"version":"2.0.0","createdAt":"2026-07-11T00:00:00Z"}}`))
			}))
			defer server.Close()

			result, err := storelookup.Default(context.Background(), storelookup.Request{
				Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: server.URL,
				HTTPClient: server.Client(), Retry: storelookup.RetryPolicy{
					MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.OnlineVersion != "2.0.0" || attempts != 2 {
				t.Fatalf("result=%#v attempts=%d", result, attempts)
			}
		})
	}
}

func TestDefaultRetriesPrivateLatestVersionConnectionReset(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"packageId":"community.lazycat.example","latestVersion":{"version":"2.0.0","createdAt":"2026-07-11T00:00:00Z"}}`))
	}))
	defer server.Close()

	httpClient := *server.Client()
	baseTransport := httpClient.Transport
	httpClient.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errors.New("read: connection reset by peer")
		}
		return baseTransport.RoundTrip(request)
	})
	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: server.URL,
		HTTPClient: &httpClient, Retry: storelookup.RetryPolicy{
			MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OnlineVersion != "2.0.0" || attempts != 2 {
		t.Fatalf("result=%#v attempts=%d", result, attempts)
	}
}

func TestDefaultRetriesPrivateLatestVersionResponseBodyReset(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"packageId":"community.lazycat.example","latestVersion":{"version":"2.0.0","createdAt":"2026-07-11T00:00:00Z"}}`))
	}))
	defer server.Close()

	httpClient := *server.Client()
	baseTransport := httpClient.Transport
	httpClient.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(io.MultiReader(
					strings.NewReader(`{"packageId":"community.lazycat.example"`),
					resetReader{},
				)),
				Request: request,
			}, nil
		}
		return baseTransport.RoundTrip(request)
	})
	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: server.URL,
		HTTPClient: &httpClient, Retry: storelookup.RetryPolicy{
			MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OnlineVersion != "2.0.0" || attempts != 2 {
		t.Fatalf("result=%#v attempts=%d", result, attempts)
	}
}

func TestDefaultStopsPrivateLatestVersionAfterRetryExhaustion(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(response, "transient upstream failure", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: server.URL,
		HTTPClient: server.Client(), Retry: storelookup.RetryPolicy{
			MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond,
		},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.StatusCode != http.StatusBadGateway || attempts != 3 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestDefaultPreservesNotFound(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	for _, store := range []storelookup.Store{storelookup.StoreOfficial, storelookup.StorePrivate} {
		t.Run(string(store), func(t *testing.T) {
			_, err := storelookup.Default(context.Background(), storelookup.Request{
				Store: store, PackageID: "cloud.lazycat.missing", BaseURL: server.URL, HTTPClient: server.Client(),
			})
			if !errors.Is(err, lpkgo.ErrNotFound) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestDefaultRejectsUnknownStore(t *testing.T) {
	_, err := storelookup.Default(context.Background(), storelookup.Request{Store: "unknown", PackageID: "cloud.lazycat.example"})
	if err == nil {
		t.Fatal("expected unknown store to fail")
	}
}

func TestDefaultDoesNotFollowPrivateRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	_, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StorePrivate, PackageID: "community.lazycat.example", BaseURL: source.URL,
		GroupCodes: []string{"ABC123"}, HTTPClient: source.Client(),
	})
	if err == nil || reached {
		t.Fatalf("err=%v reached=%v", err, reached)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type resetReader struct{}

func (resetReader) Read([]byte) (int, error) {
	return 0, errors.New("read: connection reset by peer")
}
