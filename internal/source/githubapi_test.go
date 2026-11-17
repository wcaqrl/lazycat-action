package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/githubapi"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestChangelogUsesScopedGitHubReaderWithoutReceivingTokens(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl unavailable")
	}
	t.Setenv("GITHUB_TOKEN", "do-not-forward")
	t.Setenv("LZC_API_TOKEN", "store-secret")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer managed-token" {
			t.Error("request is anonymous")
		}
		w.Write([]byte(`{"tag_name":"v1.2.3"}`))
	}))
	defer upstream.Close()
	gateway, err := githubapi.Start(t.Context(), githubapi.Client{BaseURL: upstream.URL, Token: "managed-token"}, []string{"acme/app"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	ctx := githubapi.WithEndpoint(context.Background(), gateway.Endpoint())
	settings := config.Changelog{Mode: "command", WorkingDirectory: t.TempDir(), Command: `test -z "${GITHUB_TOKEN:-}${LZC_API_TOKEN:-}"; curl --fail --silent -H "X-Lazycat-GitHub-Session: $LAZYCAT_GITHUB_API_SESSION" "$LAZYCAT_GITHUB_API_URL/repos/acme/app/releases/latest"`}
	got, err := (source.GitRunner{}).Changelog(ctx, settings, source.Candidate{Kind: "git", Tag: "v1.2.3"}, source.Candidate{}, "1.0.0", "1.2.3")
	if err != nil || got != `{"tag_name":"v1.2.3"}` {
		t.Fatalf("got=%s err=%v", got, err)
	}
}

func TestChangelogPreservesActionsMarkerWithoutForwardingToken(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_TOKEN", "must-not-forward")
	settings := config.Changelog{Mode: "command", WorkingDirectory: t.TempDir(), Command: `test -z "${GITHUB_TOKEN:-}"; test "${GITHUB_ACTIONS:-}" = true; printf 'CI marker retained'`}
	if _, err := (source.GitRunner{}).Changelog(t.Context(), settings, source.Candidate{Kind: "git", Tag: "v1.2.3"}, source.Candidate{}, "1.0.0", "1.2.3"); err != nil {
		t.Fatal(err)
	}
}
