package githubio_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/action"
	"github.com/wcaqrl/lazycat-action/internal/githubio"
)

func TestReadInputNormalizesTagVersionAndActionFields(t *testing.T) {
	environment := map[string]string{
		"INPUT_OPERATION":                 "build",
		"INPUT_CONFIG":                    ".github/lazycat-action.yml",
		"INPUT_IMAGE_ID":                  "web",
		"INPUT_VERSION":                   "v1.2.3",
		"INPUT_CHANGELOG":                 "Release notes",
		"INPUT_LPK_PATH":                  "dist/app.lpk",
		"INPUT_DOWNLOAD_URL":              "https://github.com/acme/app/releases/download/v1.2.3/app.lpk",
		"INPUT_SHA256":                    strings.Repeat("a", 64),
		"INPUT_DRY_RUN":                   "true",
		"LAZYCAT_GUARD_OFFICIAL_REVIEW":   "true",
		"GITHUB_EVENT_NAME":               "push",
		"GITHUB_REF_TYPE":                 "tag",
		"GITHUB_REF_NAME":                 "v1.2.3",
		"SOURCE_DATE_EPOCH":               "1783641600",
		"LAZYCAT_WORKFLOW_TOOLCHAINS":     "go,docker",
		"LAZYCAT_WORKFLOW_GO_VERSION":     "1.25.x",
		"LAZYCAT_WORKFLOW_NODE_VERSION":   "22.x",
		"LAZYCAT_WORKFLOW_RUST_TOOLCHAIN": "stable",
	}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Operation != action.OperationBuild || input.ConfigPath != ".github/lazycat-action.yml" || input.ImageID != "web" {
		t.Fatalf("input=%#v", input)
	}
	if input.Version != "1.2.3" || input.Tag != "v1.2.3" || !input.DryRun || !input.GuardOfficialReview || input.SourceDateEpoch != 1783641600 {
		t.Fatalf("input=%#v", input)
	}
	if input.ExpectedSHA256 != strings.Repeat("a", 64) {
		t.Fatalf("expected sha256=%q", input.ExpectedSHA256)
	}
	if input.WorkflowToolchains != "go,docker" || input.WorkflowGoVersion != "1.25.x" || input.WorkflowNodeVersion != "22.x" || input.WorkflowRustToolchain != "stable" {
		t.Fatalf("workflow toolchains=%#v", input)
	}
}

func TestReadInputAcceptsExplicitVersionForMatchingComponentTag(t *testing.T) {
	tests := []struct {
		tag     string
		version string
	}{
		{tag: "client-v0.1.38", version: "0.1.38"},
		{tag: "server-v0.1.44", version: "0.1.44"},
	}
	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			environment := map[string]string{
				"INPUT_VERSION":     test.version,
				"GITHUB_EVENT_NAME": "push",
				"GITHUB_REF_TYPE":   "tag",
				"GITHUB_REF_NAME":   test.tag,
			}
			input, err := githubio.ReadInput(func(key string) string { return environment[key] })
			if err != nil {
				t.Fatal(err)
			}
			if input.Version != test.version || input.Tag != test.tag {
				t.Fatalf("input=%#v", input)
			}
		})
	}
}

func TestReadInputRejectsExplicitVersionForUnrelatedComponentTag(t *testing.T) {
	for _, tag := range []string{"client-invalid", "client-v0.1.39"} {
		t.Run(tag, func(t *testing.T) {
			environment := map[string]string{
				"INPUT_VERSION":     "0.1.38",
				"GITHUB_EVENT_NAME": "push",
				"GITHUB_REF_TYPE":   "tag",
				"GITHUB_REF_NAME":   tag,
			}
			if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil || !strings.Contains(err.Error(), "does not identify input version") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestReadInputReadsReleaseTagFromEventFile(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"release":{"tag_name":"v2.3.4"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{"GITHUB_EVENT_NAME": "release", "GITHUB_EVENT_PATH": eventFile}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != "2.3.4" || input.Tag != "v2.3.4" {
		t.Fatalf("input=%#v", input)
	}
}

func TestReadInputReadsCanonicalTagWhenExplicitVersionIsEmpty(t *testing.T) {
	environment := map[string]string{
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_REF_TYPE":   "tag",
		"GITHUB_REF_NAME":   "v0.1.38",
	}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != "0.1.38" || input.Tag != "v0.1.38" {
		t.Fatalf("input=%#v", input)
	}
}

func TestReadInputPreservesMatchingSemVerEventTag(t *testing.T) {
	environment := map[string]string{
		"INPUT_VERSION":     "0.1.38",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_REF_TYPE":   "tag",
		"GITHUB_REF_NAME":   "0.1.38",
	}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != "0.1.38" || input.Tag != "0.1.38" {
		t.Fatalf("input=%#v", input)
	}
}

func TestReadInputRejectsExplicitVersionThatConflictsWithTagEvent(t *testing.T) {
	environment := map[string]string{
		"INPUT_VERSION":     "2.0.0",
		"GITHUB_EVENT_NAME": "push",
		"GITHUB_REF_TYPE":   "tag",
		"GITHUB_REF_NAME":   "v1.2.3",
	}
	if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil || !strings.Contains(err.Error(), "does not match event version") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadInputRejectsExplicitVersionThatConflictsWithReleaseEvent(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"release":{"tag_name":"v1.2.3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"INPUT_VERSION":     "2.0.0",
		"GITHUB_EVENT_NAME": "release",
		"GITHUB_EVENT_PATH": eventFile,
	}
	if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil || !strings.Contains(err.Error(), "does not match event version") {
		t.Fatalf("err=%v", err)
	}
}

func TestReadInputAcceptsExplicitVersionForComponentReleaseTag(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"release":{"tag_name":"client-v0.1.38"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"INPUT_VERSION":     "0.1.38",
		"GITHUB_EVENT_NAME": "release",
		"GITHUB_EVENT_PATH": eventFile,
	}
	input, err := githubio.ReadInput(func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != "0.1.38" || input.Tag != "client-v0.1.38" {
		t.Fatalf("input=%#v", input)
	}
}

func TestReadInputRejectsNonCanonicalEventTags(t *testing.T) {
	tests := []map[string]string{
		{
			"GITHUB_EVENT_NAME": "push",
			"GITHUB_REF_TYPE":   "tag",
			"GITHUB_REF_NAME":   "1.2.3",
		},
		{
			"GITHUB_EVENT_NAME": "workflow_dispatch",
			"GITHUB_REF_TYPE":   "tag",
			"GITHUB_REF_NAME":   "1.2.3",
		},
	}
	for _, environment := range tests {
		if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil || !strings.Contains(err.Error(), "must use canonical form") {
			t.Fatalf("environment=%#v err=%v", environment, err)
		}
	}
}

func TestReadInputRejectsNonCanonicalReleaseTag(t *testing.T) {
	eventFile := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"release":{"tag_name":"1.2.3"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	environment := map[string]string{
		"GITHUB_EVENT_NAME": "release",
		"GITHUB_EVENT_PATH": eventFile,
	}
	if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil || !strings.Contains(err.Error(), "must use canonical form") {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteOutputsUsesStableKeysAndDoesNotLeakSecrets(t *testing.T) {
	for key, value := range map[string]string{
		"LAZYCAT_TOKEN": "lazycat-secret", "LZC_CLI_TOKEN": "cli-secret", "LAZYCAT_PASSWORD": "password-secret",
		"LZC_API_HOST": "api.example.invalid", "LZC_API_TOKEN": "pat-secret",
	} {
		t.Setenv(key, value)
	}
	var output bytes.Buffer
	result := action.Result{
		Operation: "check", Changed: true, PackageID: "cloud.lazycat.example", PackageFile: "/tmp/package.yml", ManifestFile: "/tmp/lzc-manifest.yml", Version: "1.2.3", Tag: "v1.2.3", LPKPath: "/tmp/app.lpk",
		SHA256: strings.Repeat("a", 64), ImageResults: []byte("[]"), SourceResult: []byte(`{"kind":"git"}`), Fingerprint: "sha256:test", StateFile: "/tmp/state.yml", StoreResults: []byte(`{"official":{"published":true}}`), UpdateStrategy: "pull", Channel: "stable", ResultFile: "/tmp/result.json", RunnerArch: "arm64", TargetPlatform: "linux/amd64", OfficialStoreEnabled: true,
	}
	if err := githubio.WriteOutputs(&output, result); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, key := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "image-results", "source-result", "fingerprint", "state-file", "store-results", "official-store-enabled", "official-review-pending", "official-review-version", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if !strings.Contains(got, key+"<<lazycat_output_") {
			t.Fatalf("missing key %q in:\n%s", key, got)
		}
	}
	for _, secret := range []string{"lazycat-secret", "cli-secret", "password-secret", "pat-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("output leaked secret %q", secret)
		}
	}
}

func TestWriteOutputsChangesDelimiterWhenValueStartsWithDelimiterLine(t *testing.T) {
	var output bytes.Buffer
	result := action.Result{PackageID: "lazycat_output_2\nforged=value", ImageResults: []byte("[]")}
	if err := githubio.WriteOutputs(&output, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "package-id<<lazycat_output_2_x\n") {
		t.Fatalf("output delimiter was not changed:\n%s", output.String())
	}
}

func TestReadInputRejectsInvalidBooleanAndVersion(t *testing.T) {
	tests := []map[string]string{
		{"INPUT_DRY_RUN": "sometimes", "INPUT_VERSION": "1.2.3"},
		{"INPUT_DRY_RUN": "false", "INPUT_VERSION": "latest"},
	}
	for _, environment := range tests {
		if _, err := githubio.ReadInput(func(key string) string { return environment[key] }); err == nil {
			t.Fatalf("expected environment %#v to fail", environment)
		}
	}
}
