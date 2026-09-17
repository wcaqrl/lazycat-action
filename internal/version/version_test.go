package version_test

import (
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/version"
)

func TestInfoReportsToolkitAndCLICompatibility(t *testing.T) {
	info := version.Info()
	if info.ToolkitVersion != "v0.5.0" || info.ReferenceCLIVersion != "2.0.9" {
		t.Fatalf("info=%#v", info)
	}
}
