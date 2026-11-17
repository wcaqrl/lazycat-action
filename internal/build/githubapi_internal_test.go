package build

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"

	toolkitbuild "github.com/lib-x/lzc-toolkit-go/build"
	"github.com/wcaqrl/lazycat-action/internal/githubapi"
)

func TestPackagingGetsLiveReaderButNoPublishingTokens(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl unavailable")
	}
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
	var output bytes.Buffer
	runner := protectedRunner{base: streamingShellRunner{stdout: &output, stderr: &output}}
	err = runner.Run(githubapi.WithEndpoint(t.Context(), gateway.Endpoint()), toolkitbuild.Command{Dir: t.TempDir(), Env: map[string]string{"PATH": "/usr/bin:/bin", "GITHUB_TOKEN": "do-not-forward", "LZC_API_TOKEN": "store-secret"}, Script: `test -z "${GITHUB_TOKEN:-}${LZC_API_TOKEN:-}"; curl --fail --silent -H "X-Lazycat-GitHub-Session: $LAZYCAT_GITHUB_API_SESSION" "$LAZYCAT_GITHUB_API_URL/repos/acme/app/releases/latest"`})
	if err != nil || count.Load() != 1 {
		t.Fatalf("count=%d err=%v output=%s", count.Load(), err, output.String())
	}
}
