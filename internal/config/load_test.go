package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/config"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
		check   func(*testing.T, config.Config)
	}{
		{
			name: "minimal git project",
			yaml: `version: 1
project:
  output: dist/app.lpk
update:
  version_source:
    type: git
`,
			check: func(t *testing.T, got config.Config) {
				if got.Project.Root != "." || got.Project.BuildConfig != "lzc-build.yml" || got.Project.PackageFile != "package.yml" {
					t.Fatalf("defaults=%#v", got.Project)
				}
				if got.Project.TargetArch != "amd64" || got.Project.Target().Platform() != "linux/amd64" {
					t.Fatalf("target default=%#v", got.Project.Target())
				}
				if got.Project.Output != "dist/app.lpk" {
					t.Fatalf("output=%q", got.Project.Output)
				}
				if got.Update.Strategy != config.StrategyPull {
					t.Fatalf("strategy=%q", got.Update.Strategy)
				}
				if got.Update.AllowDowngrade {
					t.Fatal("allow_downgrade should default to false")
				}
				if !got.Build.ShouldRunBuildScript() {
					t.Fatal("buildscript should default to enabled")
				}
				if !got.Stores.Official.ShouldContinueIfNewerVersion() {
					t.Fatal("newer image versions should continue by default during official review")
				}
				if strings.Join(got.Stores.Official.Locales, ",") != "zh,en" {
					t.Fatalf("official locales=%v", got.Stores.Official.Locales)
				}
				retry := got.Stores.Official.Retry
				if retry.Enabled || retry.MaxAttempts != 3 || retry.InitialDelay != 2*time.Second || retry.MaxDelay != 30*time.Second {
					t.Fatalf("official retry=%#v", retry)
				}
			},
		},
		{
			name: "official review newer-version continuation can be disabled",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    continue_if_newer_version: false
`,
			check: func(t *testing.T, got config.Config) {
				if got.Stores.Official.ShouldContinueIfNewerVersion() {
					t.Fatal("newer-version continuation should be disabled")
				}
			},
		},
		{
			name: "image tag limit is retained",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    max_tags: 20000
    max_matching_tags: 500
`,
			check: func(t *testing.T, got config.Config) {
				if got.Images[0].MaxTags != 20000 || got.Images[0].MaxMatchingTags != 500 {
					t.Fatalf("limits=%#v", got.Images[0])
				}
			},
		},
		{
			name: "matching image tag limit exceeds raw limit",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    max_tags: 10000
    max_matching_tags: 10001
`,
			wantErr: "max_matching_tags must not exceed max_tags",
		},
		{
			name: "image tag limit exceeds cap",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    max_tags: 50001
`,
			wantErr: "max_tags must be between 1 and 50000",
		},
		{
			name: "official retry values are retained",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      max_attempts: 7
      initial_delay: 750ms
      max_delay: 45s
`,
			check: func(t *testing.T, got config.Config) {
				want := config.OfficialRetry{
					Enabled:      true,
					MaxAttempts:  7,
					InitialDelay: 750 * time.Millisecond,
					MaxDelay:     45 * time.Second,
				}
				if got.Stores.Official.Retry != want {
					t.Fatalf("official retry=%#v want %#v", got.Stores.Official.Retry, want)
				}
			},
		},
		{
			name: "malformed official retry duration",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      initial_delay: soon
`,
			wantErr: "time.Duration",
		},
		{
			name: "official retry attempts below minimum",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      max_attempts: 1
`,
			wantErr: "max_attempts must be between 2 and 10",
		},
		{
			name: "official retry attempts above maximum",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      max_attempts: 11
`,
			wantErr: "max_attempts must be between 2 and 10",
		},
		{
			name: "official retry initial delay below minimum",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      initial_delay: 99ms
`,
			wantErr: "initial_delay must be between 100ms and 1m",
		},
		{
			name: "official retry initial delay above maximum",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      initial_delay: 1m1s
`,
			wantErr: "initial_delay must be between 100ms and 1m",
		},
		{
			name: "official retry maximum delay below initial delay",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      initial_delay: 2s
      max_delay: 1s
`,
			wantErr: "max_delay must be at least initial_delay",
		},
		{
			name: "official retry maximum delay above maximum",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      max_delay: 5m1s
`,
			wantErr: "max_delay must not exceed 5m",
		},
		{
			name: "disabled official retry retains compatibility values",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: false
      max_attempts: 1
      initial_delay: 1ns
      max_delay: 2ns
`,
			check: func(t *testing.T, got config.Config) {
				retry := got.Stores.Official.Retry
				if retry.Enabled || retry.MaxAttempts != 1 || retry.InitialDelay != time.Nanosecond || retry.MaxDelay != 2*time.Nanosecond {
					t.Fatalf("official retry=%#v", retry)
				}
			},
		},
		{
			name: "unknown official retry field",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    retry:
      enabled: true
      delay_multiplier: 2
`,
			wantErr: "field delay_multiplier not found",
		},
		{
			name: "store metadata is normalized",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [ZH, en, zh]
    application:
      language: ZH
      name: " Example "
      brief: " Collaborative workspace "
      description: " Long description "
      keywords: " agents, collaboration "
      source: " https://github.com/acme/example "
      source_author: " Acme "
      support_pc: true
      support_mobile: true
      screenshot_pc_files:
        - " .github/screenshots/pc-one.png "
        - .github/screenshots/pc-two.jpg
      screenshot_mobile_files:
        - .github/screenshots/mobile-one.png
        - .github/screenshots/mobile-two.png
        - .github/screenshots/mobile-three.png
`,
			check: func(t *testing.T, got config.Config) {
				if !got.Stores.Official.SkipIfVersionExists {
					t.Fatalf("store deduplication=%#v", got.Stores)
				}
				if strings.Join(got.Stores.Official.Locales, ",") != "zh,en" {
					t.Fatalf("official locales=%v", got.Stores.Official.Locales)
				}
				application := got.Stores.Official.Application
				if application.Language != "zh" || application.Name != "Example" || application.Brief != "Collaborative workspace" || application.Description != "Long description" || application.Keywords != "agents, collaboration" {
					t.Fatalf("official application=%#v", got.Stores.Official.Application)
				}
				if !application.SupportPC || !application.SupportMobile || strings.Join(application.ScreenshotPCFiles, ",") != ".github/screenshots/pc-one.png,.github/screenshots/pc-two.jpg" || len(application.ScreenshotMobileFiles) != 3 {
					t.Fatalf("official application screenshots=%#v", application)
				}
			},
		},
		{
			name: "store deduplication defaults off",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
`,
			check: func(t *testing.T, got config.Config) {
				if got.Stores.Official.SkipIfVersionExists {
					t.Fatalf("store deduplication should default off: %#v", got.Stores)
				}
			},
		},
		{
			name: "unknown store deduplication field",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    skip_when_version_exists: true
`,
			wantErr: "field skip_when_version_exists not found",
		},
		{
			name: "all future image fields are accepted",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: nightly
    sort: created
    tag_regex: '^nightly'
    exclude_regex: arm
    version_regex: '^(?P<version>.+)$'
    version_template: '{version}'
    delivery:
      mode: mirror
      image_template: mirror.example/acme/web:{tag}
      require_digest_match: true
`,
		},
		{
			name: "unsupported target architecture",
			yaml: `version: 1
project:
  target_arch: arm
update:
  version_source:
    type: git
`,
			wantErr: "unsupported target architecture",
		},
		{
			name: "arm64 target architecture",
			yaml: `version: 1
project:
  target_arch: ARM64
update:
  version_source:
    type: git
`,
			check: func(t *testing.T, got config.Config) {
				if got.Project.TargetArch != "arm64" || got.Project.Target().Platform() != "linux/arm64" {
					t.Fatalf("project=%#v target=%#v", got.Project, got.Project.Target())
				}
			},
		},
		{
			name: "image version source",
			yaml: `version: 1
project: {}
update:
  allow_downgrade: true
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
`,
			check: func(t *testing.T, got config.Config) {
				if !got.Update.AllowDowngrade {
					t.Fatal("allow_downgrade should preserve explicit true")
				}
				image := got.Images[0]
				if image.Channel != "stable" || image.Sort != "semver" || image.VersionTemplate != "{version}" || image.Delivery.Mode != "lazycat" {
					t.Fatalf("image defaults=%#v", image)
				}
			},
		},
		{
			name: "date image defaults to semver sorting",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: date
    tag_regex: '^[0-9]{8}$'
    version_regex: '^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$'
    version_template: '{version}.{month}.{day}'
`,
			check: func(t *testing.T, got config.Config) {
				image := got.Images[0]
				if image.Channel != "date" || image.Sort != "semver" || image.VersionTemplate != "{version}.{month}.{day}" {
					t.Fatalf("image=%#v", image)
				}
			},
		},
		{
			name: "mutable image patch bump",
			yaml: `version: 1
project: {}
update:
  strategy: publish
  version_source:
    type: image
    image: web
    bump: PATCH
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: custom
    sort: created
    tag_regex: '^latest$'
`,
			check: func(t *testing.T, got config.Config) {
				if got.Update.VersionSource.Bump != "patch" {
					t.Fatalf("bump=%q", got.Update.VersionSource.Bump)
				}
			},
		},
		{
			name: "mutable bump rejects unknown strategy",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
    bump: minor
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: custom
    sort: created
    tag_regex: '^latest$'
`,
			wantErr: "unsupported version source bump",
		},
		{
			name: "mutable bump rejects git source",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
    bump: patch
`,
			wantErr: "requires type=image",
		},
		{
			name: "mutable bump rejects downgrade mode",
			yaml: `version: 1
project: {}
update:
  allow_downgrade: true
  version_source:
    type: image
    image: web
    bump: patch
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: custom
    sort: created
    tag_regex: '^latest$'
`,
			wantErr: "cannot be combined with allow_downgrade",
		},
		{
			name: "mutable bump rejects semver mapping",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
    bump: patch
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: custom
    sort: created
    tag_regex: '^latest$'
    version_regex: '^(?P<version>.*)$'
`,
			wantErr: "cannot be combined with version mapping",
		},
		{
			name: "mutable mirror requires digest verification",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
    bump: patch
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
    channel: custom
    sort: created
    tag_regex: '^latest$'
    delivery:
      mode: mirror
      image_template: mirror.example/acme/web:{tag}
`,
			wantErr: "requires require_digest_match=true",
		},
		{
			name: "stable image may prefer Docker Hub update time",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    source: docker.io/acme/web
    channel: stable
    sort: updated
`,
			check: func(t *testing.T, got config.Config) {
				if got.Images[0].Sort != "updated" {
					t.Fatalf("sort=%q", got.Images[0].Sort)
				}
			},
		},
		{
			name: "nightly image cannot use update time",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    source: docker.io/acme/web
    channel: nightly
    sort: updated
    tag_regex: '^nightly$'
`,
			wantErr: "nightly channel requires created sort",
		},
		{
			name: "path escapes root",
			yaml: `version: 1
project:
  output: ../app.lpk
update:
  version_source:
    type: git
`,
			wantErr: "output must remain beneath project root",
		},
		{
			name: "docker toolchain version is rejected",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
build:
  toolchains:
    - kind: docker
      version: "27"
`,
			wantErr: "docker toolchain version is not supported",
		},
		{
			name: "duplicate toolchains",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
build:
  toolchains:
    - kind: go
    - kind: GO
`,
			wantErr: "duplicate toolchain kind",
		},
		{
			name: "unknown toolchain",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
build:
  toolchains:
    - kind: python
`,
			wantErr: "unsupported toolchain kind",
		},
		{
			name: "duplicate image IDs",
			yaml: `version: 1
project: {}
update:
  version_source:
    type: git
images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/web
  - id: web
    target: application
    source: ghcr.io/acme/runtime
`,
			wantErr: "duplicate image id",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "lazycat.yml")
			if err := os.WriteFile(filename, []byte(test.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := config.Load(filename)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("err=%v want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.check != nil {
				test.check(t, got)
			}
		})
	}
}

func TestLoadRejectsIncompleteOfficialApplicationScreenshots(t *testing.T) {
	tests := []struct {
		name    string
		fields  string
		wantErr string
	}{
		{name: "no supported platform", fields: "brief: Example\n", wantErr: "support_pc or support_mobile"},
		{name: "brief required", fields: "support_pc: true\nscreenshot_pc_files: [one.png, two.png]\n", wantErr: "brief is required"},
		{name: "desktop needs two screenshots", fields: "brief: Example\nsupport_pc: true\nscreenshot_pc_files: [one.png]\n", wantErr: "at least two"},
		{name: "mobile needs three screenshots", fields: "brief: Example\nsupport_mobile: true\nscreenshot_mobile_files: [one.png, two.png]\n", wantErr: "at least three"},
		{name: "desktop screenshots need support", fields: "brief: Example\nsupport_mobile: true\nscreenshot_pc_files: [one.png, two.png]\nscreenshot_mobile_files: [one.png, two.png, three.png]\n", wantErr: "require support_pc=true"},
		{name: "screenshots have maximum", fields: "brief: Example\nsupport_pc: true\nscreenshot_pc_files: [1.png, 2.png, 3.png, 4.png, 5.png, 6.png, 7.png, 8.png, 9.png]\n", wantErr: "at most eight"},
		{name: "screenshot path escapes root", fields: "brief: Example\nsupport_pc: true\nscreenshot_pc_files: [../one.png, two.png]\n", wantErr: "screenshot_pc_files"},
		{name: "screenshot extension", fields: "brief: Example\nsupport_pc: true\nscreenshot_pc_files: [one.gif, two.png]\n", wantErr: "PNG or JPEG"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "lazycat.yml")
			data := "version: 1\nproject: {}\nupdate:\n  version_source:\n    type: git\nstores:\n  official:\n    enabled: true\n    create_if_missing: true\n    application:\n" + indent(test.fields, 6)
			if err := os.WriteFile(filename, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err=%v yaml=\n%s", err, data)
			}
		})
	}
}

func TestLoadRejectsInvalidImageConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		image   string
		stores  string
		wantErr string
	}{
		{name: "missing source", image: "id: web\ntarget: service\nservice: web", wantErr: "source is required"},
		{name: "unsafe id", image: "id: web/../../main\ntarget: service\nservice: web\nsource: ghcr.io/acme/web", wantErr: "must use letters"},
		{name: "service missing name", image: "id: web\ntarget: service\nsource: ghcr.io/acme/web", wantErr: "service is required"},
		{name: "application has service", image: "id: web\ntarget: application\nservice: web\nsource: ghcr.io/acme/web", wantErr: "must be empty"},
		{name: "nightly missing regex", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\nchannel: nightly", wantErr: "tag_regex is required"},
		{name: "custom missing sort", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\nchannel: custom\ntag_regex: edge", wantErr: "sort is required"},
		{name: "bad regex", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\ntag_regex: '['", wantErr: "invalid tag_regex"},
		{name: "official direct", image: "id: web\ntarget: service\nservice: web\nsource: ghcr.io/acme/web\ndelivery:\n  mode: direct", stores: "official:\n    enabled: true", wantErr: "official store requires lazycat"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			yaml := "version: 1\nproject: {}\nupdate:\n  version_source:\n    type: git\nimages:\n  - " + strings.ReplaceAll(test.image, "\n", "\n    ") + "\n"
			if test.stores != "" {
				yaml += "stores:\n" + indent(test.stores, 2)
			}
			filename := filepath.Join(t.TempDir(), "lazycat.yml")
			if err := os.WriteFile(filename, []byte(yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("err=%v yaml=\n%s", err, yaml)
			}
		})
	}
}

func TestLoadAllowsMirrorToResolveSourceAndTemplateAtRuntime(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte(`version: 1
project: {}
update:
  version_source:
    type: image
    image: web
images:
  - id: web
    target: service
    service: web
    delivery:
      mode: mirror
`)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Images[0].Source != "" || loaded.Images[0].Delivery.ImageTemplate != "" {
		t.Fatalf("image=%#v", loaded.Images[0])
	}
}

func TestLoadRejectsUnusedOfficialApplicationMetadata(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte(`version: 1
project: {}
update:
  version_source:
    type: git
stores:
  official:
    enabled: true
    application:
      name: Example
`)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), "create_if_missing") {
		t.Fatalf("err=%v", err)
	}
}

func indent(value string, spaces int) string {
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = prefix + lines[index]
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestLoadRejectsOversizedConfiguration(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte("version: 1\n#" + strings.Repeat("x", (1<<20)+1))
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err == nil {
		t.Fatal("expected oversized configuration to fail")
	}
}

func TestLoadVersion2GitSourcePipeline(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte(`version: 2
project:
  output: dist/app.lpk
source:
  kind: git
  url: git@gitee.com:example/private-app.git
  auth_ref: source
  select:
    strategy: branch-head
    branch: auto
update:
  strategy: publish
build:
  prepare:
    mode: command
    context: scripts
    command: ./scripts/build.sh
stores:
  official:
    enabled: true
`)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source.Kind != config.SourceKindGit || got.Source.Select.Branch != "auto" || got.Source.AuthRef != "source" {
		t.Fatalf("source=%#v", got.Source)
	}
	if got.State.File != ".lazycat-action.lock.yml" || got.Build.Prepare.Mode != "command" {
		t.Fatalf("state/build=%#v %#v", got.State, got.Build.Prepare)
	}
}

func TestLoadRejectsPrivateStoreField(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "lazycat.yml")
	data := []byte(`version: 1
project: {}
update:
  version_source:
    type: git
stores:
  private:
    enabled: true
`)
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(filename); err == nil || !strings.Contains(err.Error(), "field private not found") {
		t.Fatalf("err=%v", err)
	}
}
