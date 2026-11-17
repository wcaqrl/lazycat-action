package action_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/action"
	actionbuild "github.com/wcaqrl/lazycat-action/internal/build"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/githubapi"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/project"
	"github.com/wcaqrl/lazycat-action/internal/yamledit"
)

func TestRunScopesMetadataLifetimeToSuccessAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
			defer upstream.Close()
			root := t.TempDir()
			var endpoint githubapi.Endpoint
			deps := action.Dependencies{
				Host: platform.Host{OS: "linux", Arch: "amd64"}, ResultDir: filepath.Join(root, "result"),
				LoadConfig: func(string) (config.Config, error) { return gitConfig(), nil },
				Inspect: func(context.Context, config.Project) (project.Info, error) {
					return project.Info{Root: root, PackageID: "cloud.lazycat.example", Version: "1.2.3", Output: filepath.Join(root, "app.lpk")}, nil
				},
				SetVersion: func(string, string) (yamledit.Change, error) { return yamledit.Change{}, nil },
				StartGitHubAPI: func(ctx context.Context, cfg config.Config) (*githubapi.Gateway, error) {
					return githubapi.Start(ctx, githubapi.Client{BaseURL: upstream.URL, Token: "private-token"}, []string{"acme/app"})
				},
				Build: func(ctx context.Context, r actionbuild.Request) (actionbuild.Result, error) {
					endpoint = githubapi.FromContext(ctx)
					if endpoint.URL == "" {
						return actionbuild.Result{}, fmt.Errorf("reader missing")
					}
					req, _ := http.NewRequest("GET", endpoint.URL+"/repos/acme/app/releases/latest", nil)
					req.Header.Set("X-Lazycat-GitHub-Session", endpoint.Session)
					response, err := http.DefaultClient.Do(req)
					if err != nil {
						return actionbuild.Result{}, err
					}
					response.Body.Close()
					if response.StatusCode != 200 {
						return actionbuild.Result{}, fmt.Errorf("reader rejected request")
					}
					if fail {
						return actionbuild.Result{}, fmt.Errorf("deliberate build failure")
					}
					return actionbuild.Result{Path: r.Project.Output, PackageID: r.Project.PackageID, Version: r.Version, SHA256: strings.Repeat("a", 64), TargetPlatform: "linux/amd64"}, nil
				},
			}
			_, err := action.Run(t.Context(), action.Input{Operation: action.OperationBuild, Version: "1.2.3"}, deps)
			if (err != nil) != fail {
				t.Fatalf("err=%v fail=%v", err, fail)
			}
			if endpoint.URL == "" {
				t.Fatal("build did not receive reader")
			}
			if r, err := http.Get(endpoint.URL + "/repos/acme/app/releases/latest"); err == nil {
				r.Body.Close()
				t.Fatal("metadata gateway survived Action exit")
			}
		})
	}
}
