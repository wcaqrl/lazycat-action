package githubapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGatewayIsScopedReadOnlyAndCloses(t *testing.T) {
	var count atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("missing server-owned authentication")
		}
		w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer upstream.Close()
	gateway, err := Start(context.Background(), Client{BaseURL: upstream.URL, Token: "upstream-secret"}, []string{"acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := gateway.Endpoint()
	call := func(method, path, session string) int {
		req, _ := http.NewRequest(method, endpoint.URL+path, nil)
		req.Header.Set("X-Lazycat-GitHub-Session", session)
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "upstream-secret") {
			t.Fatal("upstream token exposed")
		}
		return r.StatusCode
	}
	for _, tc := range []struct{ method, path, session string }{
		{"GET", "/repos/foreign/private/releases/latest", endpoint.Session},
		{"POST", "/repos/acme/app/releases/latest", endpoint.Session},
		{"GET", "/repos/acme/app/actions/secrets", endpoint.Session},
		{"GET", "/repos/acme/app/releases/latest", ""},
		{"GET", "/repos/acme/app/releases/../latest", endpoint.Session},
		{"GET", "/repos/acme/app/releases/latest?redirect=https://evil.example", endpoint.Session},
	} {
		if code := call(tc.method, tc.path, tc.session); code < 400 {
			t.Errorf("accepted forbidden request: %#v", tc)
		}
	}
	if count.Load() != 0 {
		t.Fatal("forbidden request reached GitHub")
	}
	if code := call("GET", "/repos/acme/app/releases/latest", endpoint.Session); code != 200 || count.Load() != 1 {
		t.Fatalf("code=%d count=%d", code, count.Load())
	}
	if err := gateway.Close(); err != nil {
		t.Fatal(err)
	}
	if response, err := http.Get(endpoint.URL + "/repos/acme/app/releases/latest"); err == nil {
		response.Body.Close()
		t.Fatal("gateway stayed reachable after close")
	}
}

func TestRepositoryExtraction(t *testing.T) {
	for _, url := range []string{"https://github.com/acme/app.git", "git@github.com:acme/app.git", "ssh://git@github.com/acme/app.git"} {
		if actual := Repository(url); actual != "acme/app" {
			t.Errorf("url=%s actual=%s", url, actual)
		}
	}
	for _, url := range []string{"https://gitee.com/acme/app.git", "https://github.com.evil/acme/app", "https://github.com/acme/app/other", "https://github.com/acme/.."} {
		if Repository(url) != "" {
			t.Errorf("accepted %s", url)
		}
	}
}
