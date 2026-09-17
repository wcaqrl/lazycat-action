package action

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/platformauth"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
)

func TestPlatformStoreClientUsesToolkitPATAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v3/developer/app/check/exist" {
			t.Errorf("path=%q", request.URL.Path)
		}
		if request.Header.Get("X-API-Token") != "pat-token" || request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != "" {
			t.Errorf("headers=%v", request.Header)
		}
		_, _ = response.Write([]byte(`{"errorCode":0,"msg":"ok","data":{"exist":true}}`))
	}))
	defer server.Close()

	client, err := platformStoreClient(platformauth.Result{
		Protocol: platformauth.ProtocolPAT, Provider: auth.StaticToken("pat-token"), BaseURL: server.URL,
	}, appstore.Options{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	exists, err := client.CheckApplication(t.Context(), "cloud.lazycat.example")
	if err != nil || !exists {
		t.Fatalf("exists=%t err=%v", exists, err)
	}
}

func TestPlatformImageCopierReusesFirstAuthenticationSnapshot(t *testing.T) {
	values := map[string]string{"LAZYCAT_TOKEN": "legacy-session"}
	lookups := 0
	copier := &platformImageCopier{resolver: platformauth.Resolver{LookupEnv: func(name string) (string, bool) {
		lookups++
		value, found := values[name]
		return value, found
	}}}
	first, err := copier.clientFor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstLookups := lookups
	values = map[string]string{"LZC_API_TOKEN": "pat", "LZC_API_HOST": "new.example.invalid"}
	second, err := copier.clientFor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || lookups != firstLookups {
		t.Fatalf("client reused=%t lookups=%d want=%d", first == second, lookups, firstLookups)
	}
}

func TestLegacyImageCopyDoesNotForwardSessionAcrossRedirect(t *testing.T) {
	reachedTarget := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reachedTarget = true
		if request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != "" {
			t.Error("redirect target received legacy session credentials")
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-User-Token") != "legacy-session" {
			t.Errorf("origin X-User-Token=%q", request.Header.Get("X-User-Token"))
		}
		if cookie, err := request.Cookie("userToken"); err != nil || cookie.Value != "legacy-session" {
			t.Errorf("origin cookie=%v err=%v", cookie, err)
		}
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := platformStoreClient(platformauth.Result{
		Protocol: platformauth.ProtocolLegacySession,
		Provider: auth.StaticToken("legacy-session"),
	}, appstore.Options{BaseURL: origin.URL, HTTPClient: origin.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CopyImage(context.Background(), appstore.CopyImageRequest{
		Image: "docker.io/library/alpine:latest", Platform: "amd64",
	})
	if err == nil || !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if reachedTarget {
		t.Fatal("legacy image-copy client followed redirect")
	}
}
