package metadata_test

import (
	"os"
	"strings"
	"testing"
)

func TestRepositorySkillDescribesOfficialGenericPipelines(t *testing.T) {
	data, err := os.ReadFile("../../skills/lazycat-github-action/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"official", "version: 2", "source:", "auth_ref", "branch: auto",
		"dockerfile", "LZC_API_TOKEN", "GitHub", "Gitee", "lazycat.lock",
	} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(required)) {
			t.Fatalf("skill is missing %q", required)
		}
	}
	for _, forbidden := range []string{"APPSTORE_TOKEN", "PRIVATE_STORE_GROUP_CODES", "publish-private", "targets/poster"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("skill still contains %q", forbidden)
		}
	}
}
