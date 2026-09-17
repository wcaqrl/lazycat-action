package metadata

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestActionAndReleaseMetadataAreValidYAML(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, name := range []string{"action.yml", ".goreleaser.yml", ".github/workflows/ci.yml", ".github/workflows/lazycat.yml", ".github/workflows/release.yml"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := yaml.Unmarshal(data, &document); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if len(document) == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestReleaseWorkflowSkipsFloatingMajorTag(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "if: github.ref_name != 'v1'") {
		t.Fatal("release workflow must not publish a second release for the floating v1 tag")
	}
}

func TestReleaseWorkflowRejectsBootstrapVersionMismatch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{"Verify Action bootstrap version", "LAZYCAT_ACTION_VERSION", "github.ref_name", "action.yml", ".github/workflows/lazycat.yml", "workflow_refs"} {
		if !strings.Contains(text, required) {
			t.Fatalf("release workflow is missing bootstrap version gate %q", required)
		}
	}
}

func TestReusableWorkflowContractAndActionRefs(t *testing.T) {
	filename := filepath.Join("..", "..", ".github", "workflows", "lazycat.yml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	on, ok := document["on"].(map[string]any)
	if !ok {
		t.Fatalf("workflow on=%#v", document["on"])
	}
	call, ok := on["workflow_call"].(map[string]any)
	if !ok {
		t.Fatalf("workflow_call=%#v", on["workflow_call"])
	}
	inputs, _ := call["inputs"].(map[string]any)
	for _, name := range []string{"config", "operation", "runner", "image-id", "version", "dry-run", "changelog", "toolchains", "go-version", "node-version", "rust-toolchain", "node-package-manager", "enable-qemu", "docker-mirror", "ghcr-mirror", "registry-mirrors"} {
		if _, found := inputs[name]; !found {
			t.Fatalf("missing workflow input %q", name)
		}
	}
	secrets, _ := call["secrets"].(map[string]any)
	for _, name := range []string{"LZC_API_HOST", "LZC_API_TOKEN", "LAZYCAT_TOKEN", "APPSTORE_URL", "APPSTORE_TOKEN", "APP_ID", "PRIVATE_STORE_GROUP_CODES", "REGISTRY", "REGISTRY_USERNAME", "REGISTRY_PASSWORD"} {
		if _, found := secrets[name]; !found {
			t.Fatalf("missing workflow secret %q", name)
		}
	}
	outputs, _ := call["outputs"].(map[string]any)
	for _, name := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "store-results", "official-store-enabled", "official-review-pending", "official-review-version", "private-store-enabled", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, found := outputs[name]; !found {
			t.Fatalf("missing workflow output %q", name)
		}
	}
	permissions, _ := document["permissions"].(map[string]any)
	for _, name := range []string{"contents", "pull-requests"} {
		if got := permissions[name]; got != "write" {
			t.Fatalf("workflow permission %q=%#v, want write", name, got)
		}
	}
	jobs, _ := document["jobs"].(map[string]any)
	lazycat, _ := jobs["lazycat"].(map[string]any)
	environment, _ := lazycat["env"].(map[string]any)
	for name, want := range map[string]string{
		"LAZYCAT_DOCKER_MIRROR":    "${{ inputs.docker-mirror || vars.LAZYCAT_DOCKER_MIRROR }}",
		"LAZYCAT_GHCR_MIRROR":      "${{ inputs.ghcr-mirror || vars.LAZYCAT_GHCR_MIRROR }}",
		"LAZYCAT_REGISTRY_MIRRORS": "${{ inputs.registry-mirrors || vars.LAZYCAT_REGISTRY_MIRRORS }}",
	} {
		if got := environment[name]; got != want {
			t.Fatalf("workflow env %s=%#v want=%q", name, got, want)
		}
	}
	internalActionRefs := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "uses: ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "uses: "))
		value = strings.Fields(value)[0]
		if strings.HasPrefix(value, "wcaqrl/lazycat-action@") {
			internalActionRefs++
			if value != "wcaqrl/lazycat-action@v1" {
				t.Fatalf("reusable workflow Action ref %q must use the floating v1 tag", value)
			}
			continue
		}
		parts := strings.Split(value, "@")
		if len(parts) != 2 || !isMajorTag(parts[1]) {
			t.Fatalf("third-party Action must use a major vN tag: %s", value)
		}
	}
	if internalActionRefs != 4 {
		t.Fatalf("reusable workflow internal Action refs=%d, want 4", internalActionRefs)
	}
	workflow := string(data)
	if !strings.Contains(workflow, "version: ${{ inputs.version }}") {
		t.Fatal("reusable workflow does not forward the explicit version input")
	}
	const patMapping = "LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}"
	if count := strings.Count(workflow, patMapping); count != 3 {
		t.Fatalf("reusable workflow PAT mapping count=%d, want 3", count)
	}
	const legacyMapping = "LAZYCAT_TOKEN: ${{ secrets.LAZYCAT_TOKEN }}"
	if count := strings.Count(workflow, legacyMapping); count != 3 {
		t.Fatalf("reusable workflow legacy session mapping count=%d, want 3", count)
	}
	for _, condition := range []string{
		"steps.lazycat.outputs.operation == 'check' && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'pull'",
		"(steps.lazycat.outputs.operation == 'check' || (steps.lazycat.outputs.operation == 'build' && github.ref_type == 'branch')) && steps.lazycat.outputs.changed == 'true' && steps.lazycat.outputs.update-strategy == 'publish'",
	} {
		if !strings.Contains(workflow, condition) {
			t.Fatalf("workflow is missing operation-based update condition %q", condition)
		}
	}
	if strings.Contains(workflow, "outputs.channel != ''") {
		t.Fatal("workflow must not use channel presence to classify check operations")
	}
	privateIndex := strings.Index(workflow, "- name: Publish to MiaoMiao private store")
	officialIndex := strings.Index(workflow, "- name: Publish to LazyCat official platform")
	if privateIndex < 0 || officialIndex < 0 || privateIndex > officialIndex {
		t.Fatal("idempotent private-store publishing must run before official publishing")
	}
	mergeIndex := strings.Index(workflow, "- name: Merge store publishing results")
	if mergeIndex < officialIndex {
		t.Fatal("store result merge must run after official publishing")
	}
	if !strings.Contains(workflow[privateIndex:officialIndex], "PRIVATE_STORE_GROUP_CODES: ${{ secrets.PRIVATE_STORE_GROUP_CODES }}") {
		t.Fatal("private publish step does not receive PRIVATE_STORE_GROUP_CODES from a reusable-workflow secret")
	}
	officialStep := workflow[officialIndex:mergeIndex]
	for _, contract := range []string{
		"if: ${{ !cancelled() && steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.official-store-enabled == 'true'",
		"continue-on-error: true",
		"LAZYCAT_GUARD_OFFICIAL_REVIEW: ${{ (github.event_name == 'schedule' || github.event_name == 'workflow_dispatch') && inputs.operation == 'auto' }}",
	} {
		if !strings.Contains(officialStep, contract) {
			t.Fatalf("official publish step is missing isolation contract %q", contract)
		}
	}
	for _, contract := range []string{
		"official-review-pending: ${{ steps.lazycat.outputs.official-review-pending == 'true' || steps.publish-official.outputs.official-review-pending == 'true' }}",
		"official-review-version: ${{ steps.publish-official.outputs.official-review-version || steps.lazycat.outputs.official-review-version }}",
	} {
		if !strings.Contains(workflow, contract) {
			t.Fatalf("workflow is missing final review recheck output contract %q", contract)
		}
	}
	if strings.Contains(officialStep, "continue-on-error: ${{") {
		t.Fatal("official publish step must not depend on a runtime expression for continue-on-error")
	}
	mergeRest := workflow[mergeIndex:]
	mergeEnd := strings.Index(mergeRest, "\n      - name: ")
	if mergeEnd < 0 {
		mergeEnd = len(mergeRest)
	}
	mergeStep := mergeRest[:mergeEnd]
	for _, contract := range []string{
		"if: ${{ always() && !cancelled() }}",
		"OFFICIAL_OUTCOME: ${{ steps.publish-official.outcome }}",
		`failureReason: 'official-publish-failed'`,
		"core.warning('Official store publication failed; other configured store results are preserved.')",
		"core.summary",
	} {
		if !strings.Contains(mergeStep, contract) {
			t.Fatalf("store result merge is missing partial-failure contract %q", contract)
		}
	}
	mergeRawIndex := strings.Index(mergeStep, "Object.assign(result, parsed);")
	mergeFailureIndex := strings.Index(mergeStep, "if (process.env.OFFICIAL_OUTCOME === 'failure')")
	if mergeRawIndex < 0 || mergeFailureIndex < mergeRawIndex {
		t.Fatal("official failure marker must override any partial Action output")
	}
	enforcementIndex := strings.Index(workflow, "- name: Enforce official-only publication failure")
	if enforcementIndex < mergeIndex {
		t.Fatal("official-only failure enforcement must run after store results are merged")
	}
	enforcementRest := workflow[enforcementIndex:]
	enforcementEnd := strings.Index(enforcementRest, "\n      - name: ")
	if enforcementEnd < 0 {
		enforcementEnd = len(enforcementRest)
	}
	enforcementStep := enforcementRest[:enforcementEnd]
	for _, contract := range []string{
		"steps.publish-official.outcome == 'failure'",
		"steps.lazycat.outputs.private-store-enabled != 'true'",
		"exit 1",
	} {
		if !strings.Contains(enforcementStep, contract) {
			t.Fatalf("official-only enforcement step is missing %q", contract)
		}
	}
	for _, condition := range []string{
		"steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.official-store-enabled == 'true'",
		"steps.lazycat.outputs.update-strategy == 'publish' && steps.lazycat.outputs.private-store-enabled == 'true' && steps.store-artifact.outputs.download-url != ''",
	} {
		if !strings.Contains(workflow, condition) {
			t.Fatalf("workflow is missing store condition %q", condition)
		}
	}
	managedPaths := "add-paths: |\n            ${{ steps.lazycat.outputs.package-file }}\n            ${{ steps.lazycat.outputs.manifest-file }}"
	if !strings.Contains(workflow, managedPaths) {
		t.Fatal("workflow PR does not restrict changes to the managed package and Manifest paths")
	}
}

func isMajorTag(value string) bool {
	if len(value) < 2 || value[0] != 'v' {
		return false
	}
	for _, character := range value[1:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func TestReusableWorkflowPreparesVersionedReleaseAssets(t *testing.T) {
	filename := filepath.Join("..", "..", ".github", "workflows", "lazycat.yml")
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	on, _ := document["on"].(map[string]any)
	call, _ := on["workflow_call"].(map[string]any)
	inputs, _ := call["inputs"].(map[string]any)
	input, ok := inputs["versioned-release-asset"].(map[string]any)
	if !ok {
		t.Fatal("workflow input versioned-release-asset is missing")
	}
	if got := input["type"]; got != "boolean" {
		t.Fatalf("versioned-release-asset type=%#v, want boolean", got)
	}
	if got := input["required"]; got != false {
		t.Fatalf("versioned-release-asset required=%#v, want false", got)
	}
	if got := input["default"]; got != false {
		t.Fatalf("versioned-release-asset default=%#v, want false", got)
	}

	workflow := string(data)
	preparedPath := "${{ steps.release-asset.outputs.lpk-path }}"
	prepareIndex := strings.Index(workflow, "- name: Prepare Release asset")
	classifyIndex := strings.Index(workflow, "- name: Classify Release work")
	inspectIndex := strings.Index(workflow, "- name: Inspect existing Release Asset")
	if prepareIndex < 0 || classifyIndex < 0 || inspectIndex < 0 || classifyIndex > prepareIndex || prepareIndex > inspectIndex {
		t.Fatal("Release asset preparation must run after classification and before inspection")
	}
	prepareRest := workflow[prepareIndex:]
	prepareEnd := strings.Index(prepareRest, "\n      - name: ")
	if prepareEnd < 0 {
		prepareEnd = len(prepareRest)
	}
	prepareStep := prepareRest[:prepareEnd]
	for _, contract := range []string{
		"if: steps.release-state.outputs.should-release == 'true'",
		"LPK_PATH: ${{ steps.release-build.outputs.lpk-path }}",
		"PACKAGE_ID: ${{ steps.lazycat.outputs.package-id }}",
		"VERSION: ${{ steps.lazycat.outputs.version }}",
		"VERSIONED_RELEASE_ASSET: ${{ inputs.versioned-release-asset }}",
		`if [[ -z "${LPK_PATH}" || -z "${PACKAGE_ID}" || -z "${VERSION}" ]]`,
		`if [[ ! "${PACKAGE_ID}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ || ! "${VERSION}" =~ ^[0-9][0-9A-Za-z.+-]*$ ]]`,
		`asset_path="${LPK_PATH}"`,
		`if [[ "${VERSIONED_RELEASE_ASSET}" == "true" ]]`,
		`asset_dir="$(dirname -- "${LPK_PATH}")"`,
		`asset_path="${asset_dir}/${PACKAGE_ID}-v${VERSION}.lpk"`,
		`cp -- "${LPK_PATH}" "${asset_path}"`,
		`delimiter="lazycat_release_asset"`,
		`while grep -Fxq "${delimiter}" <<<"${asset_path}"; do`,
		`echo "lpk-path<<${delimiter}"`,
		`printf '%s\n' "${asset_path}"`,
		`echo "${delimiter}"`,
		`} >>"${GITHUB_OUTPUT}"`,
	} {
		if !strings.Contains(prepareStep, contract) {
			t.Fatalf("Release asset preparation is missing contract %q", contract)
		}
	}
	if strings.Contains(prepareStep, "GITHUB_WORKSPACE") || strings.Contains(prepareStep, "RUNNER_TEMP") || strings.Contains(prepareStep, `echo "lpk-path=${asset_path}"`) {
		t.Fatal("Release asset preparation must stay beside the verified LPK and use multiline outputs")
	}
	for _, name := range []string{
		"- name: Inspect existing Release Asset",
		"- name: Upload GitHub Release Asset",
		"- name: Resolve Release Asset URL",
	} {
		start := strings.Index(workflow, name)
		if start < 0 {
			t.Fatalf("workflow step %q is missing", name)
		}
		rest := workflow[start+len(name):]
		end := strings.Index(rest, "\n      - name: ")
		if end < 0 {
			end = len(rest)
		}
		if !strings.Contains(rest[:end], preparedPath) {
			t.Fatalf("workflow step %q does not use the prepared Release asset", name)
		}
	}
	for _, contract := range []string{
		"- name: Locate existing Release Asset for store reconciliation",
		"const assetName = `${packageId}-v${version}.lpk`;",
		"/^sha256:[0-9a-f]{64}$/",
		"- name: Download existing Release Asset for store reconciliation",
		`gh release download "${RELEASE_TAG}" --pattern "${ASSET_NAME}"`,
		`if [[ "${actual_sha256}" != "${EXPECTED_SHA256}" ]]`,
		"- name: Select verified store artifact",
		"lpk-path: ${{ steps.store-artifact.outputs.lpk-path }}",
		"download-url: ${{ steps.store-artifact.outputs.download-url }}",
		"sha256: ${{ steps.store-artifact.outputs.sha256 }}",
	} {
		if !strings.Contains(workflow, contract) {
			t.Fatalf("Release/store reconciliation is missing contract %q", contract)
		}
	}
	artifactIndex := strings.Index(workflow, "- name: Upload validation Artifact")
	if artifactIndex < 0 {
		t.Fatal("validation Artifact upload is missing")
	}
	artifactRest := workflow[artifactIndex:]
	artifactEnd := strings.Index(artifactRest, "\n      - name: ")
	if artifactEnd < 0 {
		artifactEnd = len(artifactRest)
	}
	artifactStep := artifactRest[:artifactEnd]
	if !strings.Contains(artifactStep, "path: ${{ steps.lazycat.outputs.lpk-path }}") || strings.Contains(artifactStep, preparedPath) {
		t.Fatal("validation Artifact upload must keep the Action's original LPK path")
	}
}

func TestActionMetadataExposesStableContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Inputs  map[string]any `yaml:"inputs"`
		Outputs map[string]any `yaml:"outputs"`
		Runs    struct {
			Using string `yaml:"using"`
			Steps []struct {
				Environment map[string]any `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"operation", "config", "image-id", "version", "changelog", "lpk-path", "download-url", "sha256", "dry-run", "docker-mirror", "ghcr-mirror", "registry-mirrors"} {
		if _, exists := document.Inputs[input]; !exists {
			t.Fatalf("missing input %q", input)
		}
	}
	if _, exists := document.Inputs["private-group-codes"]; exists {
		t.Fatal("private group codes must be a secret/environment variable, not an Action input")
	}
	for _, output := range []string{"operation", "changed", "package-id", "package-file", "manifest-file", "version", "tag", "lpk-path", "sha256", "download-url", "image-results", "store-results", "official-store-enabled", "official-review-pending", "official-review-version", "private-store-enabled", "update-strategy", "channel", "result-file", "runner-arch", "target-platform"} {
		if _, exists := document.Outputs[output]; !exists {
			t.Fatalf("missing output %q", output)
		}
	}
	if document.Runs.Using != "composite" {
		t.Fatalf("runs.using=%q", document.Runs.Using)
	}
	if len(document.Runs.Steps) != 1 {
		t.Fatalf("runs.steps=%d", len(document.Runs.Steps))
	}
	for name, want := range map[string]string{
		"LAZYCAT_GUARD_OFFICIAL_REVIEW": "${{ env.LAZYCAT_GUARD_OFFICIAL_REVIEW }}",
		"LAZYCAT_DOCKER_MIRROR":         "${{ inputs['docker-mirror'] || env.LAZYCAT_DOCKER_MIRROR }}",
		"LAZYCAT_GHCR_MIRROR":           "${{ inputs['ghcr-mirror'] || env.LAZYCAT_GHCR_MIRROR }}",
		"LAZYCAT_REGISTRY_MIRRORS":      "${{ inputs['registry-mirrors'] || env.LAZYCAT_REGISTRY_MIRRORS }}",
	} {
		if got := document.Runs.Steps[0].Environment[name]; got != want {
			t.Fatalf("action env %s=%#v want=%q", name, got, want)
		}
	}
	if strings.Contains(string(data), "vars.") {
		t.Fatal("composite action metadata must not reference the unsupported vars context")
	}
	if got := actionBootstrapVersion(t); got != "v1.2.10" {
		t.Fatalf("action.yml bootstrap version=%q, want v1.2.10", got)
	}
}

func actionBootstrapVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	const prefix = "LAZYCAT_ACTION_VERSION:"
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			version := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if version == "" {
				t.Fatal("action.yml bootstrap version is empty")
			}
			return version
		}
	}
	t.Fatal("action.yml bootstrap version is missing")
	return ""
}

func TestReusableWorkflowRecoversMissingReleaseForUnchangedPublish(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	for _, required := range []string{
		"PACKAGE_ID: ${{ steps.lazycat.outputs.package-id }}",
		"VERSIONED_RELEASE_ASSET: ${{ inputs.versioned-release-asset }}",
		"const expectedAssetName = `${packageId}-v${version}.lpk`;",
		"- name: Build missing Release artifact",
		"id: recovery-build",
		"operation: build",
		"version: ${{ steps.lazycat.outputs.version }}",
		"- name: Select Release build artifact",
		"id: release-build",
		"RECOVERY_LPK_PATH: ${{ steps.recovery-build.outputs.lpk-path }}",
		"LPK_PATH: ${{ steps.release-build.outputs.lpk-path }}",
		"LPK_SHA256: ${{ steps.release-build.outputs.sha256 }}",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("missing unchanged-publish Release recovery contract %q", required)
		}
	}
}

func TestReusableWorkflowPausesReleaseReconciliationForOfficialReview(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	classify := workflowStep(t, workflow, "Classify Release work")
	for _, required := range []string{
		"OFFICIAL_REVIEW_PENDING: ${{ steps.lazycat.outputs.official-review-pending }}",
		"process.env.OFFICIAL_REVIEW_PENDING === 'true'",
		"core.setOutput('should-release', 'false')",
	} {
		if !strings.Contains(classify, required) {
			t.Fatalf("official-review Release gate is missing %q", required)
		}
	}
	reconcile := workflowStep(t, workflow, "Locate existing Release Asset for store reconciliation")
	if !strings.Contains(reconcile, "steps.lazycat.outputs.official-review-pending != 'true'") {
		t.Fatal("store reconciliation must remain paused while an official review is pending")
	}
}

func TestReusableWorkflowRecoversStalePublishRerunsAndRetriesReleaseReads(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)

	commitStep := workflowStep(t, workflow, "Commit direct-publish update")
	for _, required := range []string{
		`if: (steps.lazycat.outputs.operation == 'check' || (steps.lazycat.outputs.operation == 'build' && github.ref_type == 'branch')) && steps.lazycat.outputs.changed == 'true'`,
		`remote_ref="refs/remotes/origin/${GITHUB_REF_NAME}"`,
		`git fetch --no-tags origin "+refs/heads/${GITHUB_REF_NAME}:${remote_ref}"`,
		`git diff --quiet "${remote_ref}" --`,
		`git reset --hard "${remote_ref}"`,
		`if git push origin "HEAD:${GITHUB_REF_NAME}"; then`,
		`if working_tree_matches_remote; then`,
		`adopt_matching_remote`,
		`echo "commit=$(git rev-parse HEAD)" >>"${GITHUB_OUTPUT}"`,
	} {
		if !strings.Contains(commitStep, required) {
			t.Fatalf("direct-publish rerun recovery is missing %q", required)
		}
	}
	if strings.Contains(commitStep, "git push --force") || strings.Contains(commitStep, "git push -f") {
		t.Fatal("direct-publish rerun recovery must never force-push")
	}

	for _, name := range []string{
		"Classify Release work",
		"Inspect existing Release Asset",
		"Resolve Release Asset URL",
		"Locate existing Release Asset for store reconciliation",
	} {
		step := workflowStep(t, workflow, name)
		if !strings.Contains(step, "retries: 3") {
			t.Fatalf("safe Release read step %q must retry transient GitHub 5xx responses", name)
		}
	}
}

func TestReusableWorkflowSkipsExistingRemoteReleaseTag(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	work := filepath.Join(root, "work")

	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "init", "-b", "main", work)
	runGit(t, work, "config", "user.name", "Test User")
	runGit(t, work, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(work, "package.yml"), "version: 1.3.3\n")
	runGit(t, work, "add", "package.yml")
	runGit(t, work, "commit", "-m", "release")
	runGit(t, work, "tag", "v1.3.3")
	runGit(t, work, "remote", "add", "origin", remote)
	runGit(t, work, "push", "origin", "main", "refs/tags/v1.3.3")
	writeTestFile(t, filepath.Join(work, "README.md"), "later commit\n")
	runGit(t, work, "add", "README.md")
	runGit(t, work, "commit", "-m", "later")
	runGit(t, work, "push", "origin", "main")

	script := reusableWorkflowRunScript(t, "Check remote Release tag")
	for _, test := range []struct {
		name string
		tag  string
		want string
	}{
		{name: "existing tag on older commit", tag: "v1.3.3", want: "exists=true"},
		{name: "missing tag", tag: "v1.3.4", want: "exists=false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			outputFile := filepath.Join(t.TempDir(), "github-output")
			command := exec.Command("bash", "-c", script)
			command.Dir = work
			command.Env = append(os.Environ(),
				"RELEASE_TAG="+test.tag,
				"GITHUB_OUTPUT="+outputFile,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("check remote Release tag: %v\n%s", err, output)
			}
			output, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(output)); got != test.want {
				t.Fatalf("tag check output=%q, want %q", got, test.want)
			}
		})
	}
}

func TestReusableWorkflowReusesPublishedVersionInsteadOfRebuildingIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	classifyStep := workflowStep(t, workflow, "Classify Release work")
	if strings.Contains(classifyStep, "process.env.UPDATE_STRATEGY === 'publish' && process.env.CHANGED === 'true'") {
		t.Fatal("changed publish runs must inspect the existing Release asset before scheduling Release work")
	}
	uploadStep := workflowStep(t, workflow, "Upload GitHub Release Asset")
	if !strings.Contains(uploadStep, "steps.release-tag.outputs.exists != 'true'") {
		t.Fatal("Release creation must omit a new target commit when the remote tag already exists")
	}
}

func TestDirectPublishScriptAdoptsMatchingRemoteUpdate(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	stale := filepath.Join(root, "stale")
	updater := filepath.Join(root, "updater")

	runGit(t, root, "init", "--bare", remote)
	runGit(t, root, "init", "-b", "main", seed)
	runGit(t, seed, "config", "user.name", "Test User")
	runGit(t, seed, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(seed, "package.yml"), "version: 0.2.30\n")
	writeTestFile(t, filepath.Join(seed, "lzc-manifest.yml"), "image: old\n")
	runGit(t, seed, "add", "package.yml", "lzc-manifest.yml")
	runGit(t, seed, "commit", "-m", "base")
	runGit(t, seed, "remote", "add", "origin", remote)
	runGit(t, seed, "push", "-u", "origin", "main")
	runGit(t, root, "--git-dir", remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, root, "clone", remote, stale)
	runGit(t, root, "clone", remote, updater)

	runGit(t, updater, "config", "user.name", "Updater")
	runGit(t, updater, "config", "user.email", "updater@example.com")
	writeTestFile(t, filepath.Join(updater, "package.yml"), "version: 0.2.31\n")
	writeTestFile(t, filepath.Join(updater, "lzc-manifest.yml"), "image: new\n")
	runGit(t, updater, "add", "package.yml", "lzc-manifest.yml")
	runGit(t, updater, "commit", "-m", "update")
	runGit(t, updater, "push", "origin", "main")
	remoteHead := strings.TrimSpace(runGit(t, updater, "rev-parse", "HEAD"))

	writeTestFile(t, filepath.Join(stale, "package.yml"), "version: 0.2.31\n")
	writeTestFile(t, filepath.Join(stale, "lzc-manifest.yml"), "image: new\n")
	outputFile := filepath.Join(root, "github-output")
	command := exec.Command("bash", "-c", reusableWorkflowRunScript(t, "Commit direct-publish update"))
	command.Dir = stale
	command.Env = append(os.Environ(),
		"VERSION=0.2.31",
		"PACKAGE_FILE="+filepath.Join(stale, "package.yml"),
		"MANIFEST_FILE="+filepath.Join(stale, "lzc-manifest.yml"),
		"GITHUB_REF_NAME=main",
		"GITHUB_OUTPUT="+outputFile,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run direct-publish script: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(runGit(t, stale, "rev-parse", "HEAD")); got != remoteHead {
		t.Fatalf("stale rerun HEAD=%s, want remote update %s", got, remoteHead)
	}
	if got := strings.TrimSpace(runGit(t, stale, "status", "--porcelain")); got != "" {
		t.Fatalf("stale rerun left a dirty worktree:\n%s", got)
	}
	output, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "commit="+remoteHead) {
		t.Fatalf("direct-publish output=%q, want commit=%s", output, remoteHead)
	}
}

func reusableWorkflowRunScript(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "lazycat.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, step := range document.Jobs["lazycat"].Steps {
		if step.Name == name {
			return step.Run
		}
	}
	t.Fatalf("workflow run step %q is missing", name)
	return ""
}

func runGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func writeTestFile(t *testing.T, filename, content string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func workflowStep(t *testing.T, workflow, name string) string {
	t.Helper()
	marker := "- name: " + name
	start := strings.Index(workflow, marker)
	if start < 0 {
		t.Fatalf("workflow step %q is missing", name)
	}
	rest := workflow[start:]
	end := strings.Index(rest[len(marker):], "\n      - name: ")
	if end < 0 {
		return rest
	}
	return rest[:len(marker)+end]
}

func TestActionMetadataUsesBracketSyntaxForHyphenatedNames(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expression := range []string{
		"steps.run.outputs['package-id']",
		"steps.run.outputs['package-file']",
		"steps.run.outputs['manifest-file']",
		"steps.run.outputs['target-platform']",
		"steps.run.outputs['update-strategy']",
		"inputs['image-id']",
		"inputs['download-url']",
	} {
		if !strings.Contains(text, expression) {
			t.Fatalf("action.yml is missing safe expression %q", expression)
		}
	}
}
