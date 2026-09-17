package githubio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/wcaqrl/lazycat-action/internal/action"
	"github.com/wcaqrl/lazycat-action/internal/appversion"
)

const maxEventBytes = 1 << 20

func ReadInput(getenv func(string) string) (action.Input, error) {
	if getenv == nil {
		return action.Input{}, errors.New("environment lookup is required")
	}
	input := action.Input{
		Operation:             action.Operation(strings.ToLower(strings.TrimSpace(getenv("INPUT_OPERATION")))),
		ConfigPath:            strings.TrimSpace(getenv("INPUT_CONFIG")),
		ImageID:               strings.TrimSpace(getenv("INPUT_IMAGE_ID")),
		Changelog:             getenv("INPUT_CHANGELOG"),
		LPKPath:               strings.TrimSpace(getenv("INPUT_LPK_PATH")),
		DownloadURL:           strings.TrimSpace(getenv("INPUT_DOWNLOAD_URL")),
		ExpectedSHA256:        strings.ToLower(strings.TrimSpace(getenv("INPUT_SHA256"))),
		EventName:             strings.TrimSpace(getenv("GITHUB_EVENT_NAME")),
		RefType:               strings.TrimSpace(getenv("GITHUB_REF_TYPE")),
		RefName:               strings.TrimSpace(getenv("GITHUB_REF_NAME")),
		WorkflowToolchains:    strings.TrimSpace(getenv("LAZYCAT_WORKFLOW_TOOLCHAINS")),
		WorkflowGoVersion:     strings.TrimSpace(getenv("LAZYCAT_WORKFLOW_GO_VERSION")),
		WorkflowNodeVersion:   strings.TrimSpace(getenv("LAZYCAT_WORKFLOW_NODE_VERSION")),
		WorkflowRustToolchain: strings.TrimSpace(getenv("LAZYCAT_WORKFLOW_RUST_TOOLCHAIN")),
	}
	if input.Operation == "" {
		input.Operation = action.OperationAuto
	}
	if input.ConfigPath == "" {
		input.ConfigPath = ".github/lazycat-action.yml"
	}

	dryRun := strings.TrimSpace(getenv("INPUT_DRY_RUN"))
	if dryRun != "" {
		parsed, err := strconv.ParseBool(dryRun)
		if err != nil {
			return action.Input{}, fmt.Errorf("invalid dry-run value %q", dryRun)
		}
		input.DryRun = parsed
	}
	guardOfficialReview := strings.TrimSpace(getenv("LAZYCAT_GUARD_OFFICIAL_REVIEW"))
	if guardOfficialReview != "" {
		parsed, err := strconv.ParseBool(guardOfficialReview)
		if err != nil {
			return action.Input{}, fmt.Errorf("invalid LAZYCAT_GUARD_OFFICIAL_REVIEW value %q", guardOfficialReview)
		}
		input.GuardOfficialReview = parsed
	}
	epoch := strings.TrimSpace(getenv("SOURCE_DATE_EPOCH"))
	if epoch != "" {
		parsed, err := strconv.ParseInt(epoch, 10, 64)
		if err != nil || parsed < 0 {
			return action.Input{}, fmt.Errorf("invalid SOURCE_DATE_EPOCH %q", epoch)
		}
		input.SourceDateEpoch = parsed
	}

	rawVersion := strings.TrimSpace(getenv("INPUT_VERSION"))
	eventVersion := ""
	if input.RefType == "tag" {
		eventVersion = input.RefName
	}
	if input.EventName == "release" {
		tag, err := releaseTag(strings.TrimSpace(getenv("GITHUB_EVENT_PATH")))
		if err != nil {
			return action.Input{}, err
		}
		eventVersion = tag
	}
	if rawVersion != "" {
		version, tag, err := normalizeVersion(rawVersion)
		if err != nil {
			return action.Input{}, err
		}
		if eventVersion != "" {
			eventCanonicalVersion, _, eventErr := normalizeVersion(eventVersion)
			switch {
			case eventErr == nil && eventCanonicalVersion != version:
				return action.Input{}, fmt.Errorf("input version %q does not match event version %q", version, eventCanonicalVersion)
			case eventErr != nil && !strings.HasSuffix(eventVersion, "-"+tag):
				return action.Input{}, fmt.Errorf("event tag %q does not identify input version %q", eventVersion, version)
			}
			tag = eventVersion
		}
		input.Version = version
		input.Tag = tag
		return input, nil
	}
	if eventVersion != "" {
		_, tag, err := normalizeVersion(eventVersion)
		if err != nil {
			return action.Input{}, fmt.Errorf("invalid event version: %w", err)
		}
		if eventVersion != tag {
			return action.Input{}, fmt.Errorf("event version tag %q must use canonical form %q", eventVersion, tag)
		}
	}
	rawVersion = eventVersion
	if rawVersion != "" {
		version, tag, err := normalizeVersion(rawVersion)
		if err != nil {
			return action.Input{}, err
		}
		input.Version = version
		input.Tag = tag
	}
	return input, nil
}

func WriteOutputs(writer io.Writer, result action.Result) error {
	if writer == nil {
		return errors.New("GitHub output writer is required")
	}
	imageResults := string(result.ImageResults)
	if imageResults == "" {
		imageResults = "[]"
	}
	storeResults := string(result.StoreResults)
	if storeResults == "" {
		storeResults = "{}"
	}
	outputs := []struct {
		key   string
		value string
	}{
		{key: "operation", value: result.Operation},
		{key: "changed", value: strconv.FormatBool(result.Changed)},
		{key: "package-id", value: result.PackageID},
		{key: "package-file", value: result.PackageFile},
		{key: "manifest-file", value: result.ManifestFile},
		{key: "version", value: result.Version},
		{key: "tag", value: result.Tag},
		{key: "lpk-path", value: result.LPKPath},
		{key: "sha256", value: result.SHA256},
		{key: "download-url", value: result.DownloadURL},
		{key: "image-results", value: imageResults},
		{key: "store-results", value: storeResults},
		{key: "official-store-enabled", value: strconv.FormatBool(result.OfficialStoreEnabled)},
		{key: "official-review-pending", value: strconv.FormatBool(result.OfficialReviewPending)},
		{key: "official-review-version", value: result.OfficialReviewVersion},
		{key: "private-store-enabled", value: strconv.FormatBool(result.PrivateStoreEnabled)},
		{key: "update-strategy", value: result.UpdateStrategy},
		{key: "channel", value: result.Channel},
		{key: "result-file", value: result.ResultFile},
		{key: "runner-arch", value: result.RunnerArch},
		{key: "target-platform", value: result.TargetPlatform},
	}
	for index, output := range outputs {
		delimiter := fmt.Sprintf("lazycat_output_%d", index)
		for containsLine(output.value, delimiter) {
			delimiter += "_x"
		}
		if _, err := fmt.Fprintf(writer, "%s<<%s\n%s\n%s\n", output.key, delimiter, output.value, delimiter); err != nil {
			return fmt.Errorf("write GitHub output %q: %w", output.key, err)
		}
	}
	return nil
}

func containsLine(value, target string) bool {
	for _, line := range strings.Split(value, "\n") {
		if line == target {
			return true
		}
	}
	return false
}

func WriteStepSummary(writer io.Writer, result action.Result) error {
	if writer == nil {
		return errors.New("step summary writer is required")
	}
	_, err := fmt.Fprintf(writer, "## LazyCat Action\n\n- Action host: `linux/%s`\n- LazyCat target: `%s`\n- Package: `%s`\n- Version: `%s`\n- Channel: `%s`\n- Update strategy: `%s`\n- Changed: `%t`\n- Official review pending: `%t`\n- Official review version: `%s`\n- LPK: `%s`\n", result.RunnerArch, result.TargetPlatform, result.PackageID, result.Version, result.Channel, result.UpdateStrategy, result.Changed, result.OfficialReviewPending, result.OfficialReviewVersion, result.LPKPath)
	return err
}

func releaseTag(filename string) (string, error) {
	if filename == "" {
		return "", errors.New("release event is missing GITHUB_EVENT_PATH")
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("open GitHub event file: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxEventBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return "", fmt.Errorf("read GitHub event file: %w", errors.Join(readErr, closeErr))
	}
	if len(data) > maxEventBytes {
		return "", errors.New("GitHub event file exceeds 1 MiB")
	}
	var event struct {
		Release struct {
			TagName string `json:"tag_name"`
		} `json:"release"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return "", fmt.Errorf("decode GitHub release event: %w", err)
	}
	if strings.TrimSpace(event.Release.TagName) == "" {
		return "", errors.New("GitHub release event has no tag_name")
	}
	return strings.TrimSpace(event.Release.TagName), nil
}

func normalizeVersion(raw string) (string, string, error) {
	return appversion.Normalize(raw)
}
