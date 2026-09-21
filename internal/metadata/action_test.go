package metadata_test

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestActionMetadataExposesOfficialSourcePipelineContract(t *testing.T) {
	data, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inputs  map[string]any `yaml:"inputs"`
		Outputs map[string]any `yaml:"outputs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"operation", "config", "image-id", "version", "changelog", "lpk-path", "sha256", "dry-run"} {
		if _, found := document.Inputs[input]; !found {
			t.Fatalf("missing input %q", input)
		}
	}
	for _, output := range []string{"operation", "changed", "package-id", "version", "lpk-path", "sha256", "source-result", "fingerprint", "state-file", "official-store-enabled", "official-review-pending"} {
		if _, found := document.Outputs[output]; !found {
			t.Fatalf("missing output %q", output)
		}
	}
	text := string(data)
	for _, forbidden := range []string{"publish-private", "private-store-enabled", "download-url", "APPSTORE_TOKEN", "PRIVATE_STORE_GROUP_CODES"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("action metadata still contains %q", forbidden)
		}
	}
}

func TestReusableWorkflowPublishesOnlyOfficialStoreAndPersistsState(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/lazycat.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"SOURCE_SSH_KEY:", "SOURCE_KNOWN_HOSTS:", "SOURCE_TOKEN:",
		"LAZYCAT_AUTH_SOURCE_SSH_KEY", "Set up Docker Buildx",
		"Publish to LazyCat official store", "operation: publish-official",
		"Commit packaged official update", "Commit submitted state",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"MiaoMiao", "publish-private", "APPSTORE_TOKEN", "PRIVATE_STORE_GROUP_CODES", "download-url"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("workflow still contains %q", forbidden)
		}
	}
}

func TestGiteeRunnerUsesPlatformNeutralCLI(t *testing.T) {
	data, err := os.ReadFile("../../scripts/gitee-run.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"LAZYCAT_CONFIG", "LAZYCAT_OPERATION", "LAZYCAT_PUBLISH_AFTER_CHECK", "run-action.sh", "run", "--operation", "--config", "--publish-after-check"} {
		if !strings.Contains(text, required) {
			t.Fatalf("Gitee runner is missing %q", required)
		}
	}
	info, err := os.Stat("../../scripts/gitee-run.sh")
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("Gitee runner must be executable: info=%v err=%v", info, err)
	}
}
