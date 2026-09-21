package prepare

import (
	"strings"
	"testing"
)

func TestSafeEnvironmentRemovesPublishingAndSourceCredentials(t *testing.T) {
	got := strings.Join(safeEnvironment([]string{
		"PATH=/usr/bin", "LZC_API_TOKEN=secret", "LAZYCAT_TOKEN=session", "LAZYCAT_PASSWORD=password",
		"APPSTORE_TOKEN=legacy", "LAZYCAT_AUTH_SOURCE_SSH_KEY=key", "LAZYCAT_GIT_TOKEN=git-secret",
		"REGISTRY_PASSWORD=password", "CUSTOM=value",
	}), "\n")
	if got != "PATH=/usr/bin\nCUSTOM=value" {
		t.Fatalf("environment=%q", got)
	}
}
