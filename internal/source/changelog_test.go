package source_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/source"
)

func TestChangelogUsesOnlyCommitsBetweenTags(t *testing.T) {
	directory := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", directory}, args...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "-q")
	git("commit", "--allow-empty", "-qm", "before baseline")
	git("tag", "v1.0.0")
	git("commit", "--allow-empty", "-qm", "Add a new feature")
	git("commit", "--allow-empty", "-qm", "Fix a regression")
	git("tag", "v1.1.0")
	git("commit", "--allow-empty", "-qm", "unreleased change")
	settings := config.Changelog{GitURL: filepath.ToSlash(directory)}
	candidate := source.Candidate{Kind: "oci", Tag: "v1.1.0", Revision: "sha256:dummy"}
	result, err := (source.GitRunner{}).Changelog(t.Context(), settings, candidate, source.Candidate{}, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Fix a regression") || !strings.Contains(result, "Add a new feature") || strings.Contains(result, "before baseline") || strings.Contains(result, "unreleased change") {
		t.Fatalf("unexpected changelog: %q", result)
	}
	if !strings.HasPrefix(result, "Upstream 1.1.0 (since 1.0.0)\n") {
		t.Fatalf("unexpected header: %q", result)
	}
}

func TestChangelogRejectsMissingTargetTag(t *testing.T) {
	directory := t.TempDir()
	command := exec.Command("git", "-C", directory, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, output)
	}
	_, err := (source.GitRunner{}).Changelog(t.Context(), config.Changelog{GitURL: directory}, source.Candidate{Kind: "oci", Tag: "v2.0.0"}, source.Candidate{}, "1.0.0", "2.0.0")
	if err == nil || !strings.Contains(err.Error(), "resolve changelog target") {
		t.Fatalf("expected missing target tag, got %v", err)
	}
}

func TestChangelogMatchesGitVPrefixForImageTag(t *testing.T) {
	directory := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", directory}, args...)...)
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	git("init", "-q")
	git("commit", "--allow-empty", "-qm", "First release")
	git("tag", "v1.0.0")
	git("commit", "--allow-empty", "-qm", "Add feature")
	git("tag", "v1.1.0")
	result, err := (source.GitRunner{}).Changelog(t.Context(), config.Changelog{GitURL: directory},
		source.Candidate{Kind: "oci", Tag: "1.1.0"}, source.Candidate{}, "1.0.0", "1.1.0")
	if err != nil || !strings.Contains(result, "- Add feature") || strings.Contains(result, "First release") {
		t.Fatalf("changelog=%q error=%v", result, err)
	}
}
