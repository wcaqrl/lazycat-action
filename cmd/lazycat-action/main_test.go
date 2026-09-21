package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/action"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, func(string) string { return "" }, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"toolkitVersion":"v0.5.0"`) || !strings.Contains(stdout.String(), `"referenceCliVersion":"2.0.9"`) || !strings.Contains(stdout.String(), `"targetPlatform":"linux/amd64"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"build"}, func(string) string { return "" }, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunRejectsInvalidMirrorEnvironmentBeforeLoadingConfig(t *testing.T) {
	environment := map[string]string{
		"LAZYCAT_DOCKER_MIRROR": "https://mirror.example",
	}
	var stdout, stderr bytes.Buffer
	code := run(nil, func(name string) string { return environment[name] }, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "CONFIG_INVALID") || !strings.Contains(stderr.String(), "LAZYCAT_DOCKER_MIRROR") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestFollowupPublishInputRequiresNewOfficialPublishArtifact(t *testing.T) {
	input := action.Input{ConfigPath: "lazycat-action.yml", Changelog: "Changes"}
	result := action.Result{
		Operation: string(action.OperationCheck), Changed: true, UpdateStrategy: "publish", OfficialStoreEnabled: true,
		Version: "1.2.3", Tag: "v1.2.3", LPKPath: "dist/app.lpk", SHA256: strings.Repeat("a", 64),
	}
	publish, ok := followupPublishInput(true, input, result)
	if !ok || publish.Operation != action.OperationPublishOfficial || publish.LPKPath != result.LPKPath || !publish.GuardOfficialReview || publish.Changelog != "Changes" {
		t.Fatalf("publish=%#v ok=%v", publish, ok)
	}
	result.OfficialReviewPending = true
	if _, ok := followupPublishInput(true, input, result); ok {
		t.Fatal("pending official review must prevent follow-up publication")
	}
}
