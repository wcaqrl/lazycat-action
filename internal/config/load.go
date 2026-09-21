package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/platform"
	"go.yaml.in/yaml/v3"
)

const maxConfigBytes = 1 << 20

var imageIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Load(filename string) (Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("load Action config %q: %w", filename, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Config{}, fmt.Errorf("load Action config %q: %w", filename, errors.Join(readErr, closeErr))
	}
	if len(data) > maxConfigBytes {
		return Config{}, fmt.Errorf("load Action config %q: file exceeds %d bytes", filename, maxConfigBytes)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var value Config
	if err := decoder.Decode(&value); err != nil {
		return Config{}, fmt.Errorf("decode Action config %q: %w", filename, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return Config{}, fmt.Errorf("decode Action config %q: %w", filename, err)
	}

	applyDefaults(&value)
	if err := validate(value); err != nil {
		return Config{}, fmt.Errorf("validate Action config %q: %w", filename, err)
	}
	return value, nil
}

func applyDefaults(value *Config) {
	if strings.TrimSpace(value.Project.Root) == "" {
		value.Project.Root = "."
	}
	if strings.TrimSpace(value.Project.BuildConfig) == "" {
		value.Project.BuildConfig = "lzc-build.yml"
	}
	if strings.TrimSpace(value.Project.PackageFile) == "" {
		value.Project.PackageFile = "package.yml"
	}
	if strings.TrimSpace(value.Project.Output) == "" {
		value.Project.Output = "dist/application.lpk"
	}
	if strings.TrimSpace(value.Project.TargetArch) == "" {
		value.Project.TargetArch = platform.DefaultTargetArch
	}
	if value.Version == 2 && strings.TrimSpace(value.State.File) == "" {
		value.State.File = ".lazycat-action.lock.yml"
	}
	if value.Update.Strategy == "" {
		value.Update.Strategy = StrategyPull
	}
	if value.Build.RunBuildScript == nil {
		enabled := true
		value.Build.RunBuildScript = &enabled
	}
	if value.Version == 2 && strings.TrimSpace(value.Build.Prepare.Mode) == "" {
		value.Build.Prepare.Mode = "passthrough"
	}
	if value.Stores.Official.Retry.MaxAttempts == 0 {
		value.Stores.Official.Retry.MaxAttempts = 3
	}
	if value.Stores.Official.Retry.InitialDelay == 0 {
		value.Stores.Official.Retry.InitialDelay = 2 * time.Second
	}
	if value.Stores.Official.Retry.MaxDelay == 0 {
		value.Stores.Official.Retry.MaxDelay = 30 * time.Second
	}

	value.Project.Root = filepath.Clean(strings.TrimSpace(value.Project.Root))
	value.Project.BuildConfig = filepath.Clean(strings.TrimSpace(value.Project.BuildConfig))
	value.Project.PackageFile = filepath.Clean(strings.TrimSpace(value.Project.PackageFile))
	value.Project.Output = filepath.Clean(strings.TrimSpace(value.Project.Output))
	value.Project.TargetArch = strings.ToLower(strings.TrimSpace(value.Project.TargetArch))
	value.State.File = filepath.Clean(strings.TrimSpace(value.State.File))
	value.Source.Kind = SourceKind(strings.ToLower(strings.TrimSpace(string(value.Source.Kind))))
	value.Source.URL = strings.TrimSpace(value.Source.URL)
	value.Source.Image = strings.TrimSpace(value.Source.Image)
	value.Source.AuthRef = strings.TrimSpace(value.Source.AuthRef)
	value.Source.Select.Strategy = strings.ToLower(strings.TrimSpace(value.Source.Select.Strategy))
	value.Source.Select.Branch = strings.TrimSpace(value.Source.Select.Branch)
	value.Source.Select.TagRegex = strings.TrimSpace(value.Source.Select.TagRegex)
	value.Source.Select.ExcludeRegex = strings.TrimSpace(value.Source.Select.ExcludeRegex)
	value.Source.Select.Channel = strings.ToLower(strings.TrimSpace(value.Source.Select.Channel))
	value.Source.Select.Sort = strings.ToLower(strings.TrimSpace(value.Source.Select.Sort))
	value.Build.Prepare.Mode = strings.ToLower(strings.TrimSpace(value.Build.Prepare.Mode))
	value.Build.Prepare.Command = strings.TrimSpace(value.Build.Prepare.Command)
	value.Build.Prepare.Context = strings.TrimSpace(value.Build.Prepare.Context)
	value.Build.Prepare.Dockerfile = strings.TrimSpace(value.Build.Prepare.Dockerfile)
	value.Build.Prepare.Output = strings.TrimSpace(value.Build.Prepare.Output)
	value.Update.Strategy = Strategy(strings.ToLower(strings.TrimSpace(string(value.Update.Strategy))))
	value.Update.VersionSource.Type = VersionSourceType(strings.ToLower(strings.TrimSpace(string(value.Update.VersionSource.Type))))
	value.Update.VersionSource.Image = strings.TrimSpace(value.Update.VersionSource.Image)
	value.Update.VersionSource.Bump = strings.ToLower(strings.TrimSpace(value.Update.VersionSource.Bump))
	value.Stores.Official.Locales = normalizeLocales(value.Stores.Official.Locales)
	value.Stores.Official.Application.Language = strings.ToLower(strings.TrimSpace(value.Stores.Official.Application.Language))
	if value.Stores.Official.Application.Language == "" {
		value.Stores.Official.Application.Language = "zh"
	}
	value.Stores.Official.Application.Name = strings.TrimSpace(value.Stores.Official.Application.Name)
	value.Stores.Official.Application.Brief = strings.TrimSpace(value.Stores.Official.Application.Brief)
	value.Stores.Official.Application.Description = strings.TrimSpace(value.Stores.Official.Application.Description)
	value.Stores.Official.Application.Keywords = strings.TrimSpace(value.Stores.Official.Application.Keywords)
	value.Stores.Official.Application.Source = strings.TrimSpace(value.Stores.Official.Application.Source)
	value.Stores.Official.Application.SourceAuthor = strings.TrimSpace(value.Stores.Official.Application.SourceAuthor)
	value.Stores.Official.Application.ScreenshotPCFiles = normalizeProjectPaths(value.Stores.Official.Application.ScreenshotPCFiles)
	value.Stores.Official.Application.ScreenshotMobileFiles = normalizeProjectPaths(value.Stores.Official.Application.ScreenshotMobileFiles)
	for index := range value.Build.Toolchains {
		value.Build.Toolchains[index].Kind = strings.ToLower(strings.TrimSpace(value.Build.Toolchains[index].Kind))
		value.Build.Toolchains[index].Version = strings.TrimSpace(value.Build.Toolchains[index].Version)
	}
	for index := range value.Images {
		image := &value.Images[index]
		image.ID = strings.TrimSpace(image.ID)
		image.Target = strings.ToLower(strings.TrimSpace(image.Target))
		image.Service = strings.TrimSpace(image.Service)
		image.Source = strings.TrimSpace(image.Source)
		image.Channel = strings.ToLower(strings.TrimSpace(image.Channel))
		if image.Channel == "night" {
			image.Channel = "nightly"
		}
		if image.Channel == "" {
			image.Channel = "stable"
		}
		image.Sort = strings.ToLower(strings.TrimSpace(image.Sort))
		if image.Sort == "" {
			switch image.Channel {
			case "stable", "beta", "date":
				image.Sort = "semver"
			case "nightly":
				image.Sort = "created"
			}
		}
		image.TagRegex = strings.TrimSpace(image.TagRegex)
		image.ExcludeRegex = strings.TrimSpace(image.ExcludeRegex)
		image.VersionRegex = strings.TrimSpace(image.VersionRegex)
		image.VersionTemplate = strings.TrimSpace(image.VersionTemplate)
		if image.VersionTemplate == "" {
			image.VersionTemplate = "{version}"
		}
		image.Delivery.Mode = strings.ToLower(strings.TrimSpace(image.Delivery.Mode))
		if image.Delivery.Mode == "" {
			image.Delivery.Mode = "lazycat"
		}
		image.Delivery.ImageTemplate = strings.TrimSpace(image.Delivery.ImageTemplate)
	}
}

func validate(value Config) error {
	if value.Version != 1 && value.Version != 2 {
		return fmt.Errorf("unsupported configuration version %d: expected 1 or 2", value.Version)
	}
	if err := validateRoot(value.Project.Root); err != nil {
		return err
	}
	for label, path := range map[string]string{
		"build_config": value.Project.BuildConfig,
		"package_file": value.Project.PackageFile,
		"output":       value.Project.Output,
	} {
		if err := validateProjectPath(label, path); err != nil {
			return err
		}
	}
	if !strings.EqualFold(filepath.Ext(value.Project.Output), ".lpk") {
		return errors.New("output must use the .lpk extension")
	}
	if _, err := platform.NormalizeTarget(value.Project.TargetArch); err != nil {
		return err
	}
	if value.Version == 2 {
		if value.Update.VersionSource.Type != "" || value.Update.VersionSource.Image != "" || value.Update.VersionSource.Bump != "" {
			return errors.New("version 2 pipelines use source instead of update.version_source")
		}
		if err := validateSource(value); err != nil {
			return err
		}
		if err := validateProjectPath("state.file", value.State.File); err != nil {
			return err
		}
		if err := validatePrepare(value); err != nil {
			return err
		}
	} else if value.Source.Kind != "" || value.Source.URL != "" || value.Source.Image != "" || value.Source.AuthRef != "" {
		return errors.New("source configuration requires version: 2")
	}
	if !value.Stores.Official.CreateIfMissing && hasOfficialApplication(value.Stores.Official.Application) {
		return errors.New("official application metadata requires create_if_missing=true")
	}
	if err := validateOfficialApplication(value.Stores.Official.Application); err != nil {
		return err
	}
	if value.Stores.Official.Retry.Enabled {
		retry := value.Stores.Official.Retry
		if retry.MaxAttempts < 2 || retry.MaxAttempts > 10 {
			return errors.New("official retry max_attempts must be between 2 and 10")
		}
		if retry.InitialDelay < 100*time.Millisecond || retry.InitialDelay > time.Minute {
			return errors.New("official retry initial_delay must be between 100ms and 1m")
		}
		if retry.MaxDelay < retry.InitialDelay {
			return errors.New("official retry max_delay must be at least initial_delay")
		}
		if retry.MaxDelay > 5*time.Minute {
			return errors.New("official retry max_delay must not exceed 5m")
		}
	}
	for _, locale := range value.Stores.Official.Locales {
		if !imageIDPattern.MatchString(locale) {
			return fmt.Errorf("invalid official changelog locale %q", locale)
		}
	}
	switch value.Update.Strategy {
	case StrategyPull, StrategyPublish:
	default:
		return fmt.Errorf("unsupported update strategy %q", value.Update.Strategy)
	}
	toolchains := make(map[string]struct{}, len(value.Build.Toolchains))
	for _, toolchain := range value.Build.Toolchains {
		if toolchain.Kind == "" {
			return errors.New("toolchain kind is required")
		}
		switch toolchain.Kind {
		case "go", "node", "rust", "docker":
		default:
			return fmt.Errorf("unsupported toolchain kind %q", toolchain.Kind)
		}
		if toolchain.Kind == "docker" && toolchain.Version != "" {
			return errors.New("docker toolchain version is not supported")
		}
		if _, exists := toolchains[toolchain.Kind]; exists {
			return fmt.Errorf("duplicate toolchain kind %q", toolchain.Kind)
		}
		toolchains[toolchain.Kind] = struct{}{}
	}
	images := make(map[string]struct{}, len(value.Images))
	targets := make(map[string]string, len(value.Images))
	for _, image := range value.Images {
		if image.ID == "" {
			return errors.New("image id is required")
		}
		if !imageIDPattern.MatchString(image.ID) {
			return fmt.Errorf("image id %q must use letters, digits, dot, underscore, or hyphen", image.ID)
		}
		if _, exists := images[image.ID]; exists {
			return fmt.Errorf("duplicate image id %q", image.ID)
		}
		images[image.ID] = struct{}{}
		if image.Source == "" && image.Delivery.Mode != "mirror" && value.Version != 2 {
			return fmt.Errorf("image %q source is required", image.ID)
		}
		if image.MaxTags < 0 || image.MaxTags > 50000 {
			return fmt.Errorf("image %q max_tags must be between 1 and 50000 when set", image.ID)
		}
		if image.MaxMatchingTags < 0 || image.MaxMatchingTags > 50000 {
			return fmt.Errorf("image %q max_matching_tags must be between 1 and 50000 when set", image.ID)
		}
		if image.MaxTags > 0 && image.MaxMatchingTags > image.MaxTags {
			return fmt.Errorf("image %q max_matching_tags must not exceed max_tags", image.ID)
		}
		targetKey := image.Target
		switch image.Target {
		case "service":
			if image.Service == "" {
				return fmt.Errorf("image %q service is required for service target", image.ID)
			}
			targetKey += ":" + image.Service
		case "application":
			if image.Service != "" {
				return fmt.Errorf("image %q service must be empty for application target", image.ID)
			}
		default:
			return fmt.Errorf("image %q has unsupported target %q", image.ID, image.Target)
		}
		if existing, found := targets[targetKey]; found {
			return fmt.Errorf("images %q and %q use duplicate target %q", existing, image.ID, targetKey)
		}
		targets[targetKey] = image.ID
		if err := validateImageRule(image); err != nil {
			return fmt.Errorf("image %q: %w", image.ID, err)
		}
		if value.Stores.Official.Enabled && image.Delivery.Mode != "lazycat" {
			return fmt.Errorf("official store requires lazycat delivery for image %q", image.ID)
		}
	}
	if value.Version == 2 {
		if len(value.Images) > 1 {
			return errors.New("version 2 source pipelines currently support at most one runtime image binding")
		}
		return nil
	}
	switch value.Update.VersionSource.Type {
	case VersionSourceGit:
		if value.Update.VersionSource.Image != "" {
			return errors.New("version source image must be empty when type is git")
		}
		if value.Update.VersionSource.Bump != "" {
			return errors.New("version source bump requires type=image")
		}
	case VersionSourceImage:
		if value.Update.VersionSource.Image == "" {
			return errors.New("version source image id is required")
		}
		if _, exists := images[value.Update.VersionSource.Image]; !exists {
			return fmt.Errorf("version source image %q is not configured", value.Update.VersionSource.Image)
		}
	default:
		return fmt.Errorf("unsupported version source type %q", value.Update.VersionSource.Type)
	}
	if value.Update.VersionSource.Bump != "" {
		if value.Update.VersionSource.Bump != "patch" {
			return fmt.Errorf("unsupported version source bump %q", value.Update.VersionSource.Bump)
		}
		if value.Update.AllowDowngrade {
			return errors.New("version source bump cannot be combined with allow_downgrade=true")
		}
		var versionImage *Image
		for index := range value.Images {
			if value.Images[index].ID == value.Update.VersionSource.Image {
				versionImage = &value.Images[index]
				break
			}
		}
		if versionImage == nil {
			return errors.New("version source bump requires a configured version-source image")
		}
		if versionImage.Channel != "custom" || versionImage.Sort != "created" || versionImage.TagRegex == "" {
			return errors.New("version source bump requires channel=custom, sort=created, and tag_regex")
		}
		if versionImage.VersionRegex != "" || versionImage.VersionTemplate != "{version}" {
			return errors.New("version source bump cannot be combined with version mapping")
		}
		if versionImage.Delivery.Mode == "mirror" && !versionImage.Delivery.RequireDigestMatch {
			return errors.New("version source bump with mirror delivery requires require_digest_match=true")
		}
	}
	return nil
}

func validateSource(value Config) error {
	source := value.Source
	if source.AuthRef != "" && !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`).MatchString(source.AuthRef) {
		return fmt.Errorf("source auth_ref %q contains unsupported characters", source.AuthRef)
	}
	switch source.Kind {
	case SourceKindGit:
		if source.URL == "" {
			return errors.New("Git source URL is required")
		}
		if source.Image != "" {
			return errors.New("Git source must not define image")
		}
		strategy := source.Select.Strategy
		if strategy == "" {
			strategy = "branch-head"
		}
		switch strategy {
		case "branch", "branch-head", "default-branch", "tag", "release", "semver-tag":
		default:
			return fmt.Errorf("unsupported Git source strategy %q", strategy)
		}
	case SourceKindOCI:
		if source.Image == "" {
			return errors.New("OCI source image is required")
		}
		if source.URL != "" {
			return errors.New("OCI source must not define url")
		}
		if source.AuthRef != "" {
			return errors.New("OCI source auth_ref is not supported yet; authenticate the Runner registry client instead")
		}
		if strategy := source.Select.Strategy; strategy != "" && strategy != "semver-tag" && strategy != "tag" {
			return fmt.Errorf("unsupported OCI source strategy %q", strategy)
		}
	default:
		return fmt.Errorf("unsupported source kind %q", source.Kind)
	}
	for label, pattern := range map[string]string{"tag_regex": source.Select.TagRegex, "exclude_regex": source.Select.ExcludeRegex} {
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("source %s is invalid: %w", label, err)
			}
		}
	}
	return nil
}

func validatePrepare(value Config) error {
	prepare := value.Build.Prepare
	switch prepare.Mode {
	case "passthrough":
		if value.Source.Kind != SourceKindOCI {
			return errors.New("passthrough build.prepare mode requires an OCI source")
		}
		if prepare.Command != "" || prepare.Dockerfile != "" || prepare.Context != "" || prepare.Output != "" || len(prepare.BuildArgs) > 0 {
			return errors.New("passthrough build.prepare mode must not define command, Dockerfile, context, output_image, or build_args")
		}
	case "command":
		if prepare.Command == "" {
			return errors.New("command build.prepare mode requires command")
		}
		if prepare.Dockerfile != "" {
			return errors.New("command build.prepare mode must not define dockerfile")
		}
		if prepare.Context != "" && prepare.Context != "source" {
			if err := validateProjectPath("build.prepare.context", prepare.Context); err != nil {
				return err
			}
		}
	case "dockerfile":
		if prepare.Dockerfile == "" || prepare.Output == "" {
			return errors.New("dockerfile build.prepare mode requires dockerfile and output_image")
		}
		if err := validateProjectPath("build.prepare.dockerfile", prepare.Dockerfile); err != nil {
			return err
		}
		if prepare.Context != "" && prepare.Context != "source" {
			if err := validateProjectPath("build.prepare.context", prepare.Context); err != nil {
				return err
			}
		}
		if prepare.Context == "source" && value.Source.Kind != SourceKindGit {
			return errors.New("build.prepare.context=source requires a Git source")
		}
		if prepare.Command != "" {
			return errors.New("dockerfile build.prepare mode must not define command")
		}
	default:
		return fmt.Errorf("unsupported build.prepare mode %q", prepare.Mode)
	}
	for key := range prepare.BuildArgs {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) {
			return fmt.Errorf("invalid build.prepare build_args key %q", key)
		}
	}
	if prepare.Output != "" {
		for _, placeholder := range regexp.MustCompile(`\{[^{}]+\}`).FindAllString(prepare.Output, -1) {
			switch placeholder {
			case "{version}", "{source_version}", "{revision}", "{fingerprint}":
			default:
				return fmt.Errorf("unsupported output_image placeholder %q", placeholder)
			}
		}
	}
	return nil
}

func normalizeLocales(locales []string) []string {
	if len(locales) == 0 {
		return []string{"zh", "en"}
	}
	seen := make(map[string]struct{}, len(locales))
	normalized := make([]string, 0, len(locales))
	for _, locale := range locales {
		locale = strings.ToLower(strings.TrimSpace(locale))
		if locale == "" {
			continue
		}
		if _, found := seen[locale]; found {
			continue
		}
		seen[locale] = struct{}{}
		normalized = append(normalized, locale)
	}
	if len(normalized) == 0 {
		return []string{"zh", "en"}
	}
	return normalized
}

func hasOfficialApplication(application OfficialApplication) bool {
	return application.Name != "" || application.Source != "" || application.SourceAuthor != "" || application.Language != "zh" || application.HasSubmissionInfo()
}

func normalizeProjectPaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path != "" {
			result = append(result, filepath.Clean(path))
		}
	}
	return result
}

func validateOfficialApplication(application OfficialApplication) error {
	if !application.HasSubmissionInfo() {
		return nil
	}
	if application.Brief == "" {
		return errors.New("official application brief is required for automatic information submission")
	}
	if !application.SupportPC && !application.SupportMobile {
		return errors.New("official application automatic information submission requires support_pc or support_mobile")
	}
	if application.SupportPC && len(application.ScreenshotPCFiles) < 2 {
		return errors.New("official application support_pc requires at least two screenshot_pc_files")
	}
	if !application.SupportPC && len(application.ScreenshotPCFiles) > 0 {
		return errors.New("official application screenshot_pc_files require support_pc=true")
	}
	if application.SupportMobile && len(application.ScreenshotMobileFiles) < 3 {
		return errors.New("official application support_mobile requires at least three screenshot_mobile_files")
	}
	if !application.SupportMobile && len(application.ScreenshotMobileFiles) > 0 {
		return errors.New("official application screenshot_mobile_files require support_mobile=true")
	}
	if len(application.ScreenshotPCFiles) > 8 || len(application.ScreenshotMobileFiles) > 8 {
		return errors.New("official application supports at most eight screenshots per platform")
	}
	for label, paths := range map[string][]string{
		"screenshot_pc_files": application.ScreenshotPCFiles, "screenshot_mobile_files": application.ScreenshotMobileFiles,
	} {
		for _, path := range paths {
			if err := validateProjectPath(label, path); err != nil {
				return err
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".png", ".jpg", ".jpeg":
			default:
				return fmt.Errorf("%s path %q must use PNG or JPEG", label, path)
			}
		}
	}
	return nil
}

func validateImageRule(image Image) error {
	switch image.Channel {
	case "stable", "beta", "date":
		if image.Sort != "semver" && image.Sort != "updated" {
			return fmt.Errorf("channel %q requires semver or updated sort", image.Channel)
		}
	case "nightly":
		if image.Sort != "created" {
			return errors.New("nightly channel requires created sort")
		}
		if image.TagRegex == "" {
			return errors.New("tag_regex is required for nightly channel")
		}
	case "custom":
		if image.Sort == "" {
			return errors.New("sort is required for custom channel")
		}
		if image.Sort != "semver" && image.Sort != "created" && image.Sort != "updated" {
			return fmt.Errorf("unsupported sort %q", image.Sort)
		}
		if image.TagRegex == "" {
			return errors.New("tag_regex is required for custom channel")
		}
	default:
		return fmt.Errorf("unsupported channel %q", image.Channel)
	}
	for label, expression := range map[string]string{
		"tag_regex":     image.TagRegex,
		"exclude_regex": image.ExcludeRegex,
		"version_regex": image.VersionRegex,
	} {
		if expression == "" {
			continue
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", label, err)
		}
		if label == "version_regex" && compiled.SubexpIndex("version") < 0 {
			return errors.New("version_regex must define a named version group")
		}
	}
	switch image.Delivery.Mode {
	case "lazycat", "direct":
		if image.Delivery.ImageTemplate != "" {
			return fmt.Errorf("image_template is only valid for mirror delivery")
		}
		if image.Delivery.RequireDigestMatch {
			return fmt.Errorf("require_digest_match is only valid for mirror delivery")
		}
	case "mirror":
	default:
		return fmt.Errorf("unsupported delivery mode %q", image.Delivery.Mode)
	}
	return nil
}

func validateRoot(root string) error {
	if root == "" || filepath.IsAbs(root) || root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) {
		return errors.New("project root must be a repository-relative directory")
	}
	return nil
}

func validateProjectPath(label, path string) error {
	if path == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s must remain beneath project root", label)
	}
	return nil
}
