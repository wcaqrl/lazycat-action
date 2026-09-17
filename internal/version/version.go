package version

import (
	"github.com/wcaqrl/lazycat-action/internal/platform"
	toolkitversion "github.com/lib-x/lzc-toolkit-go/version"
)

var ActionVersion = "dev"

const (
	ToolkitVersion      = "v" + toolkitversion.SDKVersion
	ReferenceCLIPackage = toolkitversion.ReferenceCLIPackage
	ReferenceCLIVersion = toolkitversion.ReferenceCLIVersion
)

type BuildInfo struct {
	ActionVersion       string `json:"actionVersion"`
	ToolkitVersion      string `json:"toolkitVersion"`
	ReferenceCLIPackage string `json:"referenceCliPackage"`
	ReferenceCLIVersion string `json:"referenceCliVersion"`
	TargetPlatform      string `json:"targetPlatform"`
}

func Info() BuildInfo {
	return BuildInfo{
		ActionVersion:       ActionVersion,
		ToolkitVersion:      ToolkitVersion,
		ReferenceCLIPackage: ReferenceCLIPackage,
		ReferenceCLIVersion: ReferenceCLIVersion,
		TargetPlatform:      platform.TargetPlatform,
	}
}
