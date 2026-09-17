package platform

import (
	"fmt"
	"strings"
)

const (
	TargetOS          = "linux"
	DefaultTargetArch = "amd64"
	TargetArch        = DefaultTargetArch
	TargetPlatform    = TargetOS + "/" + TargetArch
)

type Target struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func NormalizeTarget(arch string) (Target, error) {
	arch = strings.ToLower(strings.TrimSpace(arch))
	if arch == "" {
		arch = DefaultTargetArch
	}
	if arch != "amd64" && arch != "arm64" {
		return Target{}, fmt.Errorf("unsupported target architecture %q: supported values are amd64 and arm64", arch)
	}
	return Target{OS: TargetOS, Arch: arch}, nil
}

func (target Target) Platform() string {
	if target.OS == "" || target.Arch == "" {
		return ""
	}
	return target.OS + "/" + target.Arch
}

func (target Target) Normalize() (Target, error) {
	if target.OS != "" && target.OS != TargetOS {
		return Target{}, fmt.Errorf("unsupported target OS %q: only linux is supported", target.OS)
	}
	return NormalizeTarget(target.Arch)
}

type Host struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func NormalizeHost(goos, goarch string) (Host, error) {
	if goos != "linux" {
		return Host{}, fmt.Errorf("unsupported Action host OS %q: only linux is supported", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return Host{}, fmt.Errorf("unsupported Action host architecture %q: supported values are amd64 and arm64", goarch)
	}
	return Host{OS: goos, Arch: goarch}, nil
}
