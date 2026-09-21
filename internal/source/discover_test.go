package source_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestGitRunnerDiscoversRemoteDefaultBranch(t *testing.T) {
	var calls [][]string
	runner := source.GitRunner{Run: func(_ context.Context, args, environment []string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		if !contains(environment, "GIT_TERMINAL_PROMPT=0") {
			t.Fatalf("environment=%v", environment)
		}
		if reflect.DeepEqual(args, []string{"ls-remote", "--symref", "git@gitee.com:acme/app.git", "HEAD"}) {
			return []byte("ref: refs/heads/master\tHEAD\n1111111111111111111111111111111111111111\tHEAD\n"), nil
		}
		return []byte("2222222222222222222222222222222222222222\trefs/heads/master\n"), nil
	}}
	candidate, err := runner.Discover(t.Context(), config.Source{
		Kind: config.SourceKindGit, URL: "git@gitee.com:acme/app.git",
		Select: config.SourceSelect{Strategy: "branch-head", Branch: "auto"},
	}, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Branch != "master" || candidate.Ref != "refs/heads/master" || candidate.Revision != strings.Repeat("2", 40) || candidate.Version != "" {
		t.Fatalf("candidate=%#v", candidate)
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
}

func TestGitRunnerSelectsHighestStableSemVerTagAndPeeledCommit(t *testing.T) {
	runner := source.GitRunner{Run: func(_ context.Context, args, _ []string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"ls-remote", "--tags", "https://github.com/acme/app.git"}) {
			t.Fatalf("args=%v", args)
		}
		return []byte(strings.Join([]string{
			strings.Repeat("1", 40) + "\trefs/tags/v1.2.0",
			strings.Repeat("2", 40) + "\trefs/tags/v1.3.0",
			strings.Repeat("3", 40) + "\trefs/tags/v1.3.0^{}",
			strings.Repeat("4", 40) + "\trefs/tags/v2.0.0-beta.1",
		}, "\n")), nil
	}}
	candidate, err := runner.Discover(t.Context(), config.Source{
		Kind: config.SourceKindGit, URL: "https://github.com/acme/app.git",
		Select: config.SourceSelect{Strategy: "semver-tag", TagRegex: `^v[0-9]+\.[0-9]+\.[0-9]+$`},
	}, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Version != "1.3.0" || candidate.Tag != "v1.3.0" || candidate.Revision != strings.Repeat("3", 40) {
		t.Fatalf("candidate=%#v", candidate)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
