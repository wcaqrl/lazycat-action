package config

import (
	"strings"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/platform"
)

type Strategy string

const (
	StrategyPull    Strategy = "pull"
	StrategyPublish Strategy = "publish"
)

type VersionSourceType string

const (
	VersionSourceGit   VersionSourceType = "git"
	VersionSourceImage VersionSourceType = "image"
)

type Config struct {
	Version int     `yaml:"version"`
	Project Project `yaml:"project"`
	Update  Update  `yaml:"update"`
	Build   Build   `yaml:"build"`
	Images  []Image `yaml:"images"`
	Stores  Stores  `yaml:"stores"`
}

type Project struct {
	Root        string `yaml:"root"`
	BuildConfig string `yaml:"build_config"`
	PackageFile string `yaml:"package_file"`
	Output      string `yaml:"output"`
	TargetArch  string `yaml:"target_arch"`
}

func (project Project) Target() platform.Target {
	arch := strings.ToLower(strings.TrimSpace(project.TargetArch))
	if arch == "" {
		arch = platform.DefaultTargetArch
	}
	return platform.Target{OS: platform.TargetOS, Arch: arch}
}

type Update struct {
	Strategy       Strategy      `yaml:"strategy"`
	AllowDowngrade bool          `yaml:"allow_downgrade"`
	VersionSource  VersionSource `yaml:"version_source"`
}

type VersionSource struct {
	Type  VersionSourceType `yaml:"type"`
	Image string            `yaml:"image"`
	Bump  string            `yaml:"bump"`
}

type Build struct {
	Toolchains     []Toolchain `yaml:"toolchains"`
	RunBuildScript *bool       `yaml:"run_buildscript"`
}

type Toolchain struct {
	Kind    string `yaml:"kind"`
	Version string `yaml:"version"`
}

type Image struct {
	ID              string   `yaml:"id"`
	Target          string   `yaml:"target"`
	Service         string   `yaml:"service"`
	Source          string   `yaml:"source"`
	Channel         string   `yaml:"channel"`
	Sort            string   `yaml:"sort"`
	TagRegex        string   `yaml:"tag_regex"`
	ExcludeRegex    string   `yaml:"exclude_regex"`
	VersionRegex    string   `yaml:"version_regex"`
	VersionTemplate string   `yaml:"version_template"`
	MaxTags         int      `yaml:"max_tags"`
	MaxMatchingTags int      `yaml:"max_matching_tags"`
	Delivery        Delivery `yaml:"delivery"`
}

type Delivery struct {
	Mode               string `yaml:"mode"`
	ImageTemplate      string `yaml:"image_template"`
	RequireDigestMatch bool   `yaml:"require_digest_match"`
}

type Stores struct {
	Official OfficialStore `yaml:"official"`
	Private  PrivateStore  `yaml:"private"`
}

type OfficialStore struct {
	Enabled                bool                `yaml:"enabled"`
	SkipIfVersionExists    bool                `yaml:"skip_if_version_exists"`
	ContinueIfNewerVersion *bool               `yaml:"continue_if_newer_version"`
	CreateIfMissing        bool                `yaml:"create_if_missing"`
	Locales                []string            `yaml:"changelog_locales"`
	Application            OfficialApplication `yaml:"application"`
	Retry                  OfficialRetry       `yaml:"retry"`
}

func (store OfficialStore) ShouldContinueIfNewerVersion() bool {
	return store.ContinueIfNewerVersion == nil || *store.ContinueIfNewerVersion
}

type OfficialRetry struct {
	Enabled      bool          `yaml:"enabled"`
	MaxAttempts  int           `yaml:"max_attempts"`
	InitialDelay time.Duration `yaml:"initial_delay"`
	MaxDelay     time.Duration `yaml:"max_delay"`
}

type OfficialApplication struct {
	Language              string   `yaml:"language"`
	Name                  string   `yaml:"name"`
	Brief                 string   `yaml:"brief"`
	Description           string   `yaml:"description"`
	Keywords              string   `yaml:"keywords"`
	Source                string   `yaml:"source"`
	SourceAuthor          string   `yaml:"source_author"`
	SupportPC             bool     `yaml:"support_pc"`
	SupportMobile         bool     `yaml:"support_mobile"`
	ScreenshotPCFiles     []string `yaml:"screenshot_pc_files"`
	ScreenshotMobileFiles []string `yaml:"screenshot_mobile_files"`
}

func (application OfficialApplication) HasSubmissionInfo() bool {
	return application.Brief != "" || application.Description != "" || application.Keywords != "" ||
		application.SupportPC || application.SupportMobile || len(application.ScreenshotPCFiles) > 0 || len(application.ScreenshotMobileFiles) > 0
}

type PrivateStore struct {
	Enabled             bool   `yaml:"enabled"`
	SkipIfVersionExists bool   `yaml:"skip_if_version_exists"`
	Name                string `yaml:"name"`
	Summary             string `yaml:"summary"`
}

func (build Build) ShouldRunBuildScript() bool {
	return build.RunBuildScript == nil || *build.RunBuildScript
}
