package metadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestRepositorySkillContractAndEvals(t *testing.T) {
	root := filepath.Join("..", "..", "skills", "lazycat-github-action")
	for _, name := range []string{
		"SKILL.md", "agents/openai.yaml", "references/configuration.md", "references/workflows.md",
		"assets/lazycat-action.yml", "assets/lazycat-workflow.yml", "evals/evals.json",
	} {
		if info, err := os.Stat(filepath.Join(root, name)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("skill file %q: info=%v err=%v", name, info, err)
		}
	}
	skill, err := os.ReadFile(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(skill)
	frontmatterEnd := strings.Index(text[4:], "\n---")
	if !strings.HasPrefix(text, "---\n") || frontmatterEnd < 0 {
		t.Fatal("SKILL.md has invalid frontmatter")
	}
	frontmatter := text[:frontmatterEnd+4]
	for _, required := range []string{"historical LPK migration or cleanup", "Go Template Manifest preservation"} {
		if !strings.Contains(frontmatter, required) {
			t.Fatalf("SKILL.md frontmatter missing trigger %q", required)
		}
	}
	for _, required := range []string{"name: lazycat-github-action", "automatically inspect", "Primary outcome: working GitHub workflows", "Do not stop after printing sample YAML", "Do not infer", "linux/amd64", "project.target_arch", "sort: updated", "Docker Hub", "last_updated", "APPSTORE_TOKEN", "LZC_API_HOST", "LZC_API_TOKEN", "LAZYCAT_TOKEN", "lzc-cli session", "skip_if_version_exists", "PRIVATE_STORE_GROUP_CODES", "onlineVersion", "Repository overrides Organization", "delivery source of truth", "{version}.{build}.0", "allow_downgrade: false", "VERSION_DOWNGRADE_BLOCKED", "rank filtered tag names", "first usable", "lazycat-contrib/cat-led", "lazycat-contrib/lazycat-neko-webshell", "failed to spawn protoc", "edition = \"2023\""} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing %q", required)
		}
	}
	for _, required := range []string{"Node.js 24", "actions/checkout@v7", "actions/setup-node@v7"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing Node.js 24 Action contract %q", required)
		}
	}
	for _, forbidden := range []string{"actions/checkout@v4", "actions/setup-node@v4"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("SKILL.md contains deprecated Node.js 20 Action %q", forbidden)
		}
	}
	for _, required := range []string{
		"Both repository entry points are supported",
		"wcaqrl/lazycat-action@v1",
		"wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1",
		"caller owns checkout, permissions, toolchains, Release handling",
		"complete automation path",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing interface contract %q", required)
		}
	}
	var evals struct {
		Version int `json:"version"`
		Cases   []struct {
			ID             string   `json:"id"`
			Prompt         string   `json:"prompt"`
			MustInclude    []string `json:"mustInclude"`
			MustNotInclude []string `json:"mustNotInclude"`
		} `json:"cases"`
	}
	data, err := os.ReadFile(filepath.Join(root, "evals", "evals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &evals); err != nil {
		t.Fatal(err)
	}
	if evals.Version != 1 || len(evals.Cases) < 8 {
		t.Fatalf("eval metadata=%#v", evals)
	}
	seen := make(map[string]struct{}, len(evals.Cases))
	mixedTagEvalContract := ""
	for _, eval := range evals.Cases {
		if eval.ID == "" || eval.Prompt == "" || len(eval.MustInclude) == 0 || len(eval.MustNotInclude) == 0 {
			t.Fatalf("incomplete eval=%#v", eval)
		}
		if _, found := seen[eval.ID]; found {
			t.Fatalf("duplicate eval id %q", eval.ID)
		}
		seen[eval.ID] = struct{}{}
		if eval.ID == "mixed-weekly-lts-version-selection" {
			negativeAssertions := strings.Join(eval.MustNotInclude, "\n")
			for _, ambiguous := range []string{"sort: updated", "allow_downgrade: true", "custom script"} {
				if strings.Contains(negativeAssertions, ambiguous) {
					t.Fatalf("mixed weekly/LTS eval must not reject correct prohibitive language %q", ambiguous)
				}
			}
			mixedTagEvalContract = strings.Join([]string{
				eval.Prompt,
				strings.Join(eval.MustInclude, "\n"),
				negativeAssertions,
			}, "\n")
		}
	}
	if _, found := seen["dual-store-version-deduplication"]; !found {
		t.Fatal("evals are missing dual-store version deduplication coverage")
	}
	if _, found := seen["automatic-workflow-generation"]; !found {
		t.Fatal("evals are missing automatic workflow generation coverage")
	}
	if _, found := seen["release-store-reconciliation"]; !found {
		t.Fatal("evals are missing Release/store reconciliation coverage")
	}
	if _, found := seen["rust-protobuf-toolchain"]; !found {
		t.Fatal("evals are missing Rust Protobuf toolchain coverage")
	}
	if _, found := seen["official-retry-and-failure-isolation"]; !found {
		t.Fatal("evals are missing official retry and failure-isolation coverage")
	}
	if _, found := seen["official-review-newer-candidate-continuation"]; !found {
		t.Fatal("evals are missing official review newer-candidate continuation coverage")
	}
	if _, found := seen["updated-tag-and-target-architecture"]; !found {
		t.Fatal("evals are missing updated-tag and target-architecture coverage")
	}
	if _, found := seen["mutable-latest-patch-bump"]; !found {
		t.Fatal("evals are missing mutable latest patch-bump coverage")
	}
	if _, found := seen["node24-action-runtime"]; !found {
		t.Fatal("evals are missing Node.js 24 Action runtime coverage")
	}
	if _, found := seen["pat-default-legacy-opt-in"]; !found {
		t.Fatal("evals are missing PAT-default legacy-authentication coverage")
	}
	if _, found := seen["evolvable-version-selection"]; !found {
		t.Fatal("evals are missing evolvable version-selection coverage")
	}
	if _, found := seen["major-line-version-selection"]; !found {
		t.Fatal("evals are missing major-line version-selection coverage")
	}
	if _, found := seen["tag-filter-version-mapping-separation"]; !found {
		t.Fatal("evals are missing tag-filter/version-mapping separation coverage")
	}
	if _, found := seen["date-tag-leading-zero-normalization"]; !found {
		t.Fatal("evals are missing date-tag leading-zero normalization coverage")
	}
	if _, found := seen["mixed-weekly-lts-version-selection"]; !found {
		t.Fatal("evals are missing mixed weekly/LTS version-selection coverage")
	}
	for _, required := range []string{
		"two-part weekly", "three-part LTS", "JDK", "latest",
		"channel: custom", "sort: semver", "(?P<version>", "{version}.0", "allow_downgrade: false",
		"sort: created", "channel: nightly", "tag_regex: '^2\\.578$'",
	} {
		if !strings.Contains(mixedTagEvalContract, required) {
			t.Fatalf("mixed weekly/LTS eval contract missing %q", required)
		}
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md", "assets/lazycat-action.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if !strings.Contains(text, "skip_if_version_exists") {
			t.Fatalf("%s is missing skip_if_version_exists", name)
		}
		if !strings.Contains(text, "continue_if_newer_version") {
			t.Fatalf("%s is missing continue_if_newer_version", name)
		}
	}
	workflow, err := os.ReadFile(filepath.Join(root, "references", "workflows.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), "PRIVATE_STORE_GROUP_CODES") || !strings.Contains(string(workflow), "GitHub Secret") || !strings.Contains(string(workflow), "Repository overrides Organization") {
		t.Fatal("workflow reference must document private group codes as a GitHub Secret")
	}
	for _, required := range []string{
		"historical LPK migration",
		"Go Template Manifest",
		"git ls-files '*.lpk'",
		"total bytes",
		"before deleting tracked LPKs",
		"declines",
		"preserve every tracked LPK",
		"*.lpk",
		"versioned-release-asset: true",
		"<package-id>-v<version>.lpk",
		"never execute or evaluate",
		"if`, `else`, `end`, `with`, and `range",
		"fail closed",
		"Do Not",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing expanded contract %q", required)
		}
	}
	checkpoint := markdownSection(text, "## 🔴 CHECKPOINT — before deleting tracked LPKs")
	for _, required := range []string{
		"git ls-files '*.lpk'",
		"count",
		"total bytes",
		"STOP",
		"explicit yes/no answer immediately before deletion",
		"declines",
		"preserve every tracked LPK",
	} {
		if !strings.Contains(checkpoint, required) {
			t.Fatalf("historical-LPK checkpoint missing %q", required)
		}
	}
	downgradeCheckpoint := markdownSection(text, "## 🔴 CHECKPOINT — before enabling version downgrades")
	for _, required := range []string{"current package version", "selected lower version", "STOP", "explicit yes/no answer", "allow_downgrade: true", "declines"} {
		if !strings.Contains(downgradeCheckpoint, required) {
			t.Fatalf("version-downgrade checkpoint missing %q", required)
		}
	}
	readmes := []struct {
		name     string
		heading  string
		required []string
	}{
		{"README.md", "## Using the Skill", []string{"package.yml", "lzc-build.yml", "Manifest", ".github/lazycat-action.yml", ".github/workflows/*.yml", "pauses", "GitHub Secret"}},
		{"README.zh-CN.md", "## 使用 Skill", []string{"package.yml", "lzc-build.yml", "Manifest", ".github/lazycat-action.yml", ".github/workflows/*.yml", "暂停", "GitHub Secret"}},
	}
	for _, readme := range readmes {
		data, err := os.ReadFile(filepath.Join("..", "..", readme.name))
		if err != nil {
			t.Fatal(err)
		}
		section := markdownSection(string(data), readme.heading)
		for _, required := range readme.required {
			if !strings.Contains(section, required) {
				t.Fatalf("%s Skill usage section missing %q", readme.name, required)
			}
		}
	}
	for _, name := range []string{"README.md", "README.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, required := range []string{
			"wcaqrl/lazycat-action@v1",
			"wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1",
			"Composite Action",
			"Reusable Workflow",
			"Node.js 24",
			"v2.327.1",
			"actions/checkout@v7",
			"actions/setup-node@v7",
			"sort: updated",
			"target_arch",
		} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s missing supported interface %q", name, required)
			}
		}
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "versioned-release-asset") {
			t.Fatalf("%s is missing versioned-release-asset", name)
		}
	}
	configuration, err := os.ReadFile(filepath.Join(root, "references", "configuration.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"(?P<build>", "{version}.{build}.0", "Unknown placeholders", "non-SemVer", "sort: updated", "last_updated", "target_arch", "arm64"} {
		if !strings.Contains(string(configuration), required) {
			t.Fatalf("configuration reference missing named version template contract %q", required)
		}
	}
	starter, err := os.ReadFile(filepath.Join(root, "assets", "lazycat-workflow.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(starter), "versioned-release-asset: true") {
		t.Fatal("scheduled pull workflow starter must not enable versioned-release-asset")
	}
	for _, name := range []string{"references/configuration.md", "references/workflows.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"Go Template", "never evaluat", "fail closed"} {
			if !strings.Contains(string(data), required) {
				t.Fatalf("%s is missing template contract %q", name, required)
			}
		}
	}
	prompts, err := os.ReadFile(filepath.Join(root, "test-prompts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var promptCases []struct {
		ID       string `json:"id"`
		Prompt   string `json:"prompt"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(prompts, &promptCases); err != nil {
		t.Fatal(err)
	}
	if len(promptCases) != 22 {
		t.Fatalf("test-prompts.json cases=%d, want 22", len(promptCases))
	}
	promptIDs := make(map[string]string, len(promptCases))
	for _, prompt := range promptCases {
		if prompt.ID == "" || prompt.Prompt == "" || prompt.Expected == "" {
			t.Fatalf("incomplete test prompt=%#v", prompt)
		}
		if _, found := promptIDs[prompt.ID]; found {
			t.Fatalf("duplicate test prompt id %q", prompt.ID)
		}
		promptIDs[prompt.ID] = prompt.Expected
	}
	for id, required := range map[string][]string{
		"historical-lpk-migration":              {"git ls-files '*.lpk'", "总字节", "yes/no", "拒绝", "versioned-release-asset: true", "<package-id>-v<version>.lpk"},
		"go-template-manifest-preservation":     {"绝不执行或求值", "if/else/end/with/range", "逐字节", "fail closed"},
		"release-store-reconciliation":          {"精确命名", "GitHub sha256 digest", "本地 SHA256", "官方商店补交", "喵喵商店", "独立跳过", "不重建", "不改名", "不猜测"},
		"official-file-upload-stage":            {"本地 LPK 文件", "multipart", "store.official.upload", "store.official.review", "不得打印"},
		"named-version-template-groups":         {"version", "build", "{version}.{build}.0", "20260603.1.0", "fail closed"},
		"date-tag-leading-zero-normalization":   {"channel: date", "tag_regex", "version_regex", "month", "day", "2026.6.26", "2026.1.1", "前导零", "兼容"},
		"private-name-fallback":                 {"stores.private.name", "packageId", "应用名称", "/api/v1/apps/by-name", "404", "停止"},
		"image-version-downgrade-guard":         {"allow_downgrade: false", "SemVer", "VERSION_DOWNGRADE_BLOCKED", "同版本", "明确确认"},
		"rust-protobuf-toolchain":               {"Edition 2023", "GitHub Release", "SHA256", "protoc --version", "共享 buildscript", "不得把 Proto 改成 proto3", "不得修改 Rust 源码", "build.rs"},
		"store-online-version-downgrade-guard":  {"allow_downgrade: false", "SemVer", "7.8.138", "7.7.406", "online-version-newer", "version-already-online", "non-SemVer", "独立"},
		"official-retry-and-failure-isolation":  {"enabled: false", "max_attempts", "initial_delay", "max_delay", "429", "5xx", "审核网络错误或 5xx 不重放", "400", "双商店", "warning", "官方唯一目标", "message"},
		"updated-tag-and-target-architecture":   {"sort: updated", "last_updated", "v1.2.15", "v1.2.26", "target_arch", "amd64", "arm64", "allow_downgrade: false"},
		"node24-action-runtime":                 {"Node.js 24", "actions/checkout@v7", "actions/setup-node@v7", "不得生成", "@v4"},
		"pat-default-legacy-opt-in":             {"LZC_API_TOKEN", "默认", "LAZYCAT_TOKEN", "仅", "lzc-cli 会话", "不得同时映射"},
		"evolvable-version-selection":           {"tag_regex", "标签族", "2.x", "version_regex", "sort", "latest", "不得", "^2\\.2\\.0$"},
		"major-line-version-selection":          {"v2", "^v?2\\.\\d+\\.\\d+$", "semver", "未来", "2.2.0"},
		"tag-filter-version-mapping-separation": {"tag_regex", "version_regex", "(?P<version>", "version_template", "筛选", "映射"},
		"mixed-weekly-lts-version-selection":    {"两段周更", "三段 LTS", "JDK", "channel: custom", "sort: semver", "version_regex", "{version}.0", "allow_downgrade: false", "不得使用 sort: updated"},
		"official-image-project":                {"continue_if_newer_version: true", "等于或高于", "更旧", "同一候选", "最终校验 LPK", "再次认证复查", "Git 版本源", "SemVer"},
	} {
		expected, found := promptIDs[id]
		if !found {
			t.Fatalf("test-prompts.json missing %q", id)
		}
		for _, value := range required {
			if !strings.Contains(expected, value) {
				t.Fatalf("test prompt %q expected result missing %q", id, value)
			}
		}
	}
	for _, required := range []string{
		"Do not map `LAZYCAT_TOKEN` by default",
		"existing caller still depends on an lzc-cli session token",
		"Do not map both credentials as a generic fallback",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing PAT-default authentication rule %q", required)
		}
	}
	for _, required := range []string{
		"A version-discovery rule must match a release family",
		"Do not generate an exact immutable SemVer filter such as `^2\\.2\\.0$`",
		"Exact tag filters are reserved for mutable channel names",
		"intentional one-version pin",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing evolvable version-selection rule %q", required)
		}
	}
	configurationText := string(configuration)
	for _, required := range []string{
		"filter a release family rather than the currently selected immutable version",
		"^v?2\\.\\d+\\.\\d+$",
		"not an automatic update strategy",
		"Mixed immutable tag families",
		"2.578-jdk21",
		"version_template: '{version}.0'",
		"lazycat-contrib/jenkins-lzcapp",
	} {
		if !strings.Contains(configurationText, required) {
			t.Fatalf("configuration reference missing evolvable version-selection rule %q", required)
		}
	}
	workflowReference := string(workflow)
	for _, required := range []string{
		"New and generated callers map `LZC_API_TOKEN` only",
		"Do not add `LAZYCAT_TOKEN` as a redundant fallback",
		"Legacy-only compatibility",
	} {
		if !strings.Contains(workflowReference, required) {
			t.Fatalf("workflow reference missing PAT-default authentication rule %q", required)
		}
	}
	starterText := string(starter)
	if strings.Contains(starterText, "LAZYCAT_TOKEN") {
		t.Fatal("workflow starter must not mention or map LAZYCAT_TOKEN")
	}
	exampleWorkflows, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(exampleWorkflows) == 0 {
		t.Fatal("no example workflows found")
	}
	for _, filename := range exampleWorkflows {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		example := string(data)
		if strings.Contains(example, "secrets: inherit") {
			t.Fatalf("%s must explicitly map only required secrets", filename)
		}
		if strings.Contains(example, "LAZYCAT_TOKEN:") {
			t.Fatalf("%s must not map legacy LAZYCAT_TOKEN by default", filename)
		}
	}
	storesExample, err := os.ReadFile(filepath.Join("..", "..", "examples", "stores", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(storesExample), "LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}") {
		t.Fatal("stores example must map the preferred LZC_API_TOKEN PAT")
	}
	reusableWorkflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	reusableText := string(reusableWorkflow)
	if !strings.Contains(reusableText, "actions/checkout@v7") || !strings.Contains(reusableText, "actions/setup-node@v7") {
		t.Fatal("reusable workflow must use Node.js 24-compatible checkout/setup-node v7 Actions")
	}
	for _, required := range []string{
		"actions/setup-go@v7",
		"actions/upload-artifact@v7",
		"actions/github-script@v9",
		"docker/login-action@v4",
		"docker/setup-qemu-action@v4",
		"docker/setup-buildx-action@v4",
		"peter-evans/create-pull-request@v8",
		"softprops/action-gh-release@v3",
	} {
		if !strings.Contains(reusableText, required) {
			t.Fatalf("reusable workflow is missing current GitHub Action major %q", required)
		}
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md is missing current GitHub Action major %q", required)
		}
	}
	for _, forbidden := range []string{
		"actions/setup-go@v6",
		"actions/upload-artifact@v6",
		"actions/github-script@v8",
		"docker/login-action@v3",
		"docker/setup-qemu-action@v3",
		"docker/setup-buildx-action@v3",
		"peter-evans/create-pull-request@v7",
		"softprops/action-gh-release@v2",
	} {
		if strings.Contains(reusableText, forbidden) || strings.Contains(text, forbidden) {
			t.Fatalf("workflow or SKILL.md still references superseded GitHub Action major %q", forbidden)
		}
	}
	releaseWorkflow, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"actions/checkout@v7",
		"docker/setup-qemu-action@v4",
		"anchore/sbom-action/download-syft@v0",
		"actions/attest-build-provenance@v4",
	} {
		if !strings.Contains(string(releaseWorkflow), required) || !strings.Contains(text, required) {
			t.Fatalf("release workflow and SKILL.md must use current GitHub Action major %q", required)
		}
	}
	if strings.Contains(string(releaseWorkflow), "actions/attest-build-provenance@v3") || strings.Contains(text, "actions/attest-build-provenance@v3") {
		t.Fatal("release workflow or SKILL.md still references attest-build-provenance v3")
	}
	for _, forbidden := range []string{"actions/checkout@v4", "actions/setup-node@v4"} {
		if strings.Contains(reusableText, forbidden) {
			t.Fatalf("reusable workflow contains deprecated Node.js 20 Action %q", forbidden)
		}
	}
	for _, name := range []string{"docs/gitea-forgejo-actions.md", "docs/gitea-forgejo-actions.zh-CN.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		document := string(data)
		for _, required := range []string{"Node.js 24", "https://github.com/actions/checkout@v7"} {
			if !strings.Contains(document, required) {
				t.Fatalf("%s missing Node.js 24 Action contract %q", name, required)
			}
		}
		if strings.Contains(document, "https://github.com/actions/checkout@v4") {
			t.Fatalf("%s contains deprecated Node.js 20 checkout Action", name)
		}
	}
	activeActionFiles := []string{
		".github/workflows/ci.yml",
		".github/workflows/lazycat.yml",
		".github/workflows/release.yml",
		"docs/gitea-forgejo-actions.md",
		"docs/gitea-forgejo-actions.zh-CN.md",
		"skills/lazycat-github-action/SKILL.md",
		"skills/lazycat-github-action/references/workflows.md",
	}
	for _, name := range activeActionFiles {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		for lineNumber, line := range strings.Split(string(data), "\n") {
			for _, action := range []string{"actions/checkout@", "actions/setup-node@"} {
				if strings.Contains(line, action) && !strings.Contains(line, action+"v7") {
					t.Fatalf("%s:%d uses a non-v7 Node.js Action: %s", name, lineNumber+1, strings.TrimSpace(line))
				}
			}
		}
	}
	contractFiles := []string{"SKILL.md", "references/configuration.md", "references/workflows.md"}
	for _, name := range contractFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		contract := string(data)
		for _, forbidden := range []string{"both stores receive the same", "both store consumers use the same", "both stores resolve the same", "both stores use the same"} {
			if strings.Contains(contract, forbidden) {
				t.Fatalf("%s contains inaccurate store URL contract %q", name, forbidden)
			}
		}
	}
	for _, name := range []string{"SKILL.md", "references/configuration.md", "references/workflows.md"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		contract := string(data)
		for _, required := range []string{"online-version-newer", "version-already-online", "allow_downgrade: false", "non-SemVer"} {
			if !strings.Contains(contract, required) {
				t.Fatalf("%s is missing store downgrade reconciliation contract %q", name, required)
			}
		}
	}
	for _, required := range []string{"private store uses the verified GitHub Release Asset URL and SHA256", "official store uploads the same locally verified LPK bytes and SHA256 without receiving the Release URL"} {
		if !strings.Contains(text, required) {
			t.Fatalf("SKILL.md missing store asset contract %q", required)
		}
	}
	for _, name := range contractFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		contract := string(data)
		for _, required := range []string{"<package-id>-v<version>.lpk", "sha256", "store"} {
			if !strings.Contains(contract, required) {
				t.Fatalf("%s is missing Release/store reconciliation contract %q", name, required)
			}
		}
	}
	for _, name := range []string{"README.md", "README.zh-CN.md", filepath.Join("skills", "lazycat-github-action", "SKILL.md"), filepath.Join("skills", "lazycat-github-action", "references", "configuration.md"), filepath.Join("skills", "lazycat-github-action", "references", "workflows.md")} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		contract := string(data)
		for _, required := range []string{"retry", "enabled: false", "max_attempts", "initial_delay", "max_delay", "429", "5xx", "400", "container_name", "message"} {
			if !strings.Contains(contract, required) {
				t.Fatalf("%s is missing official retry/isolation contract %q", name, required)
			}
		}
	}
	starterConfig, err := os.ReadFile(filepath.Join(root, "assets", "lazycat-action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var starterDocument struct {
		Stores struct {
			Official struct {
				ContinueIfNewerVersion *bool          `yaml:"continue_if_newer_version"`
				Retry                  map[string]any `yaml:"retry"`
			} `yaml:"official"`
		} `yaml:"stores"`
	}
	if err := yaml.Unmarshal(starterConfig, &starterDocument); err != nil {
		t.Fatal(err)
	}
	if len(starterDocument.Stores.Official.Retry) != 1 || starterDocument.Stores.Official.Retry["enabled"] != false {
		t.Fatalf("starter official retry=%#v, want only enabled: false", starterDocument.Stores.Official.Retry)
	}
	if starterDocument.Stores.Official.ContinueIfNewerVersion == nil || !*starterDocument.Stores.Official.ContinueIfNewerVersion {
		t.Fatalf("starter official continue_if_newer_version=%v, want true", starterDocument.Stores.Official.ContinueIfNewerVersion)
	}
}

func TestOfficialScreenshotSubmissionDocumentationContract(t *testing.T) {
	root := filepath.Join("..", "..")
	files := map[string][]string{
		filepath.Join("skills", "lazycat-github-action", "SKILL.md"): {
			"screenshot_pc_files", "screenshot_mobile_files", "Both support flags default to false",
			"authenticated developer API", "pending review", "commit and push", "project-confirmed viewport",
			"2-8 screenshots", "3-8", "15 MiB", "320-3840", "center-crops it to 16:9",
		},
		filepath.Join("skills", "lazycat-github-action", "references", "configuration.md"): {
			"/api/v3/developer/app/list", "screenshot_pc_files", "screenshot_mobile_files",
			"Both support flags default to false", "Information already approved", "Review already pending",
			"CONFLICT", "commit and push", "Remote URLs are unsupported", "15 MiB", "320-3840",
		},
		"README.md": {
			"screenshot_pc_files", "screenshot_mobile_files", "agent-browser", "committed and pushed",
			"public catalog", "pending review", "15 MiB", "320 and 3840", "center-cropped to 16:9",
		},
		"README.zh-CN.md": {
			"screenshot_pc_files", "screenshot_mobile_files", "agent-browser", "提交并推送",
			"匿名公开目录", "待审核任务", "15 MiB", "320-3840", "居中裁剪为 16:9",
		},
	}
	for name, requiredValues := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, required := range requiredValues {
			if !strings.Contains(text, required) {
				t.Fatalf("%s is missing official screenshot contract %q", name, required)
			}
		}
	}
}

func markdownSection(text, heading string) string {
	start := strings.Index(text, heading)
	if start < 0 {
		return ""
	}
	rest := text[start+len(heading):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		return rest[:end]
	}
	return rest
}
