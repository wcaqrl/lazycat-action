package private_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/store/private"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const (
	packageID   = "cloud.lazycat.example.app"
	version     = "1.2.3"
	downloadURL = "https://github.com/acme/example/releases/download/v1.2.3/app.lpk"
	digest      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestClientCreatesApplicationWithExternalVersion(t *testing.T) {
	var created bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer lcst_test" {
			t.Errorf("authorization=%q", request.Header.Get("Authorization"))
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/apps":
			query := request.URL.Query().Get("q")
			if query != packageID && query != "Example App" {
				t.Errorf("q=%q", request.URL.Query().Get("q"))
			}
			_, _ = response.Write([]byte(`{"apps":[]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/apps":
			created = true
			body, _ := io.ReadAll(request.Body)
			text := string(body)
			for _, expected := range []string{`"packageId":"` + packageID + `"`, `"version":"1.2.3"`, `"sourceType":"GITHUB"`, `"downloadUrl":"` + downloadURL + `"`, `"sha256":"` + digest + `"`} {
				if !strings.Contains(text, expected) {
					t.Errorf("request body=%s missing %s", text, expected)
				}
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"app":{"id":17,"packageId":"cloud.lazycat.example.app","latestVersion":{"id":23,"appId":17,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{
		PackageID: packageID, Name: "Example App", Summary: "Published from CI", Version: version,
		Changelog: "Release notes", DownloadURL: downloadURL, SHA256: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !result.Published || !result.Created || result.Existing || result.AppID != "17" || result.VersionID != "23" || result.PackageID != packageID || result.Version != version || result.DownloadURL != downloadURL || result.SHA256 != digest {
		t.Fatalf("created=%v result=%#v", created, result)
	}
}

func TestClientReusesExistingVersionByAppID(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts++
		}
		_, _ = response.Write([]byte(`{"app":{"id":42,"packageId":"cloud.lazycat.example.app","versions":[{"id":55,"appId":42,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}}`))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{AppID: "42", PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if err != nil {
		t.Fatal(err)
	}
	if posts != 0 || !result.Published || !result.Existing || result.Created || result.VersionID != "55" {
		t.Fatalf("posts=%d result=%#v", posts, result)
	}
}

func TestClientCreatesExternalVersionForExistingApp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			_, _ = response.Write([]byte(`{"app":{"id":"42","packageId":"cloud.lazycat.example.app","versions":[]}}`))
		case http.MethodPost:
			if request.URL.Path != "/api/v1/apps/42/versions" || request.Header.Get("Content-Type") != "application/json" {
				t.Errorf("path=%q content-type=%q", request.URL.Path, request.Header.Get("Content-Type"))
			}
			body, _ := io.ReadAll(request.Body)
			if strings.Contains(string(body), "file") || !strings.Contains(string(body), `"downloadUrl":"`+downloadURL+`"`) {
				t.Errorf("body=%s", body)
			}
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"version":{"id":56,"appId":42,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
		}
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{AppID: "42", PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if err != nil || result.VersionID != "56" || result.Created || result.Existing {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestClientResolvesExistingApplicationByNameWhenHistoricalPackageDiffers(t *testing.T) {
	const appName = "Existing App"
	const historicalPackageID = "community.lazycat.historical.app"
	packageQueries := 0
	nameResolutions := 0
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/apps":
			packageQueries++
			if query := request.URL.Query().Get("q"); query != packageID {
				t.Errorf("package query=%q", query)
			}
			_, _ = response.Write([]byte(`{"apps":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/apps/by-name":
			nameResolutions++
			if name := request.URL.Query().Get("name"); name != appName {
				t.Errorf("name=%q", name)
			}
			_, _ = response.Write([]byte(`{"app":{"id":42,"packageId":"` + historicalPackageID + `","name":"Existing App","canUploadVersion":true}}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/apps/42":
			_, _ = response.Write([]byte(`{"app":{"id":42,"packageId":"` + historicalPackageID + `","name":"Existing App","versions":[]}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/apps/42/versions":
			posts++
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{"version":{"id":56,"appId":42,"version":"1.2.3","downloadUrl":"https://github.com/acme/example/releases/download/v1.2.3/app.lpk","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Publish(context.Background(), private.Request{
		PackageID: packageID, Name: appName, Version: version, DownloadURL: downloadURL, SHA256: digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if packageQueries != 1 || nameResolutions != 1 || posts != 1 || result.AppID != "42" || result.VersionID != "56" || result.PackageID != historicalPackageID || result.Created || result.Existing {
		t.Fatalf("packageQueries=%d nameResolutions=%d posts=%d result=%#v", packageQueries, nameResolutions, posts, result)
	}
}

func TestClientStopsWhenNameResolutionIsAmbiguous(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts++
		}
		if request.URL.Path == "/api/v1/apps" {
			_, _ = response.Write([]byte(`{"apps":[]}`))
			return
		}
		if request.URL.Path == "/api/v1/apps/by-name" {
			response.WriteHeader(http.StatusConflict)
			_, _ = response.Write([]byte(`{"error":{"code":"APP_NAME_AMBIGUOUS","message":"Multiple writable apps have this name"}}`))
			return
		}
		http.NotFound(response, request)
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Publish(context.Background(), private.Request{
		PackageID: packageID, Name: "Duplicate App", Version: version, DownloadURL: downloadURL, SHA256: digest,
	})
	if !errors.Is(err, lpkgo.ErrConflict) || posts != 0 {
		t.Fatalf("err=%v posts=%d", err, posts)
	}
}

func TestClientValidatesReleaseURLAndSHA(t *testing.T) {
	tests := []struct {
		name string
		url  string
		sha  string
	}{
		{name: "http", url: "http://github.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "host", url: "https://example.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "path", url: "https://github.com/acme/example/archive/v1.zip", sha: digest},
		{name: "userinfo", url: "https://user@github.com/acme/example/releases/download/v1/app.lpk", sha: digest},
		{name: "fragment", url: downloadURL + "#secret", sha: digest},
		{name: "sha", url: downloadURL, sha: "bad"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := private.New(private.Options{BaseURL: "https://store.example.com", Token: "lcst_test"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: test.url, SHA256: test.sha})
			if !errors.Is(err, lpkgo.ErrInvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestClientDoesNotExposeRemoteAuthenticationBody(t *testing.T) {
	const secretBody = "token lcst_must_not_leak"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		_, _ = response.Write([]byte(secretBody))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if !errors.Is(err, lpkgo.ErrUnauthenticated) || strings.Contains(err.Error(), secretBody) {
		t.Fatalf("err=%v", err)
	}
}

func TestClientDoesNotForwardTokenAcrossRedirect(t *testing.T) {
	reached := false
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
		forwarded = request.Header.Get("Authorization") != ""
		_, _ = response.Write([]byte(`{"apps":[]}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client, err := private.New(private.Options{BaseURL: origin.URL, Token: "lcst_test", HTTPClient: origin.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if err == nil || reached || forwarded {
		t.Fatalf("err=%v reached=%v forwarded=%v", err, reached, forwarded)
	}
}

func TestClientRejectsOversizedAndMalformedResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
	}{
		{name: "oversized", body: strings.Repeat("x", (1<<20)+1)},
		{name: "malformed", body: "not-json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Publish(context.Background(), private.Request{PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
			if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestClientRejectsInvalidRemoteIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			t.Fatal("client must not use an invalid remote application ID")
		}
		_, _ = response.Write([]byte(`{"app":{"id":"../admin","packageId":"cloud.lazycat.example.app","versions":[]}}`))
	}))
	defer server.Close()
	client, err := private.New(private.Options{BaseURL: server.URL, Token: "lcst_test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Publish(context.Background(), private.Request{AppID: "42", PackageID: packageID, Name: "Example", Version: version, DownloadURL: downloadURL, SHA256: digest})
	if !errors.Is(err, lpkgo.ErrRemoteUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
