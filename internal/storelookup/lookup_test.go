package storelookup_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/wcaqrl/lazycat-action/internal/storelookup"
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
	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StoreOfficial, PackageID: "cloud.lazycat.example", BaseURL: server.URL, HTTPClient: server.Client(),
	})
	if err != nil || result.OnlineVersion != "1.2.3" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestDefaultRetriesTransientOfficialResponses(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(response, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = response.Write([]byte(`{"package":"cloud.lazycat.example","version":{"name":"1.2.3","package":"cloud.lazycat.example"}}`))
	}))
	defer server.Close()
	result, err := storelookup.Default(context.Background(), storelookup.Request{
		Store: storelookup.StoreOfficial, PackageID: "cloud.lazycat.example", BaseURL: server.URL, HTTPClient: server.Client(),
		Retry: storelookup.RetryPolicy{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
	})
	if err != nil || result.OnlineVersion != "1.2.3" || attempts != 2 {
		t.Fatalf("result=%#v attempts=%d err=%v", result, attempts, err)
	}
}

func TestDefaultRejectsUnknownStore(t *testing.T) {
	_, err := storelookup.Default(context.Background(), storelookup.Request{Store: "unknown", PackageID: "cloud.lazycat.example"})
	if !errors.Is(err, lpkgo.ErrInvalidArgument) {
		t.Fatalf("err=%v", err)
	}
}
