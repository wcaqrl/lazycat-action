package prepare_test

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/githubapi"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/prepare"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestPreparationGetsLiveReaderButNoPublishingTokens(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl unavailable")
	}
	t.Setenv("GITHUB_TOKEN", "do-not-forward")
	t.Setenv("LZC_API_TOKEN", "store-secret")
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Authorization") != "Bearer managed-token" {
			t.Error("anonymous request")
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	gateway, err := githubapi.Start(t.Context(), githubapi.Client{BaseURL: server.URL, Token: "managed-token"}, []string{"acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	command := `test -z "${GITHUB_TOKEN:-}${LZC_API_TOKEN:-}"; curl --fail --silent -H "X-Lazycat-GitHub-Session: $LAZYCAT_GITHUB_API_SESSION" "$LAZYCAT_GITHUB_API_URL/repos/acme/app/releases/latest" >/dev/null`
	_, err = (prepare.Runner{}).Prepare(githubapi.WithEndpoint(t.Context(), gateway.Endpoint()), prepare.Request{
		ProjectRoot: t.TempDir(), Config: config.Config{Source: config.Source{Kind: config.SourceKindOCI}, Build: config.Build{Prepare: config.Prepare{Mode: "command", Command: command}}},
		Candidate: source.Candidate{Kind: "oci", Ref: "example/app:1"}, Target: platform.Target{OS: "linux", Arch: "amd64"},
	})
	if err != nil || count.Load() != 1 {
		t.Fatalf("count=%d err=%v", count.Load(), err)
	}
}
