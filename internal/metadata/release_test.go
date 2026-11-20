package metadata_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestReusableWorkflowPinsItsActionAndPublisherToBootstrapVersion(t *testing.T) {
	raw, err := os.ReadFile("../../action.yml")
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	if err := yaml.Unmarshal(raw, &action); err != nil {
		t.Fatal(err)
	}
	version := ""
	for _, step := range action.Runs.Steps {
		if v := step.Env["LAZYCAT_ACTION_VERSION"]; v != "" {
			version = v
		}
	}
	if version == "" {
		t.Fatal("bootstrap version missing")
	}
	raw, err = os.ReadFile("../../.github/workflows/lazycat.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "lazycat-adapter/lazycat-action@") {
				found++
				if step.Uses != "lazycat-adapter/lazycat-action@"+version {
					t.Errorf("Action %s does not match binary %s", step.Uses, version)
				}
			}
			if step.With["repository"] == "lazycat-adapter/lazycat-action" && step.With["ref"] != version {
				t.Errorf("publisher %s does not match binary %s", step.With["ref"], version)
			}
		}
	}
	if found < 2 {
		t.Fatal("both build and publication must pin the released Action")
	}
}

func TestPrereleaseNeverPromotesFloatingV1(t *testing.T) {
	root := t.TempDir()
	record := filepath.Join(root, "git-operations")
	binary := filepath.Join(root, "git")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_GIT_RECORD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"v1.10.0-rc.1", "v2.0.0"} {
		command := exec.Command("bash", "../../scripts/promote-v1.sh", tag)
		command.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "TEST_GIT_RECORD="+record)
		if raw, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", tag, err, raw)
		}
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("non-stable release invoked git")
	}
	command := exec.Command("bash", "../../scripts/promote-v1.sh", "v1.10.0")
	command.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "TEST_GIT_RECORD="+record)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, raw)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "refs/tags/v1") || !strings.Contains(string(raw), "v1.10.0^{commit}") {
		t.Fatal("stable release did not promote the tested commit")
	}
}

func TestAdapterStagesDoNotPersistCheckoutCredentials(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/lazycat.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string            `yaml:"name"`
				With map[string]string `yaml:"with"`
				Env  map[string]string `yaml:"env"`
				Run  string            `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Name == "Checkout pipeline project" && step.With["persist-credentials"] != "false" {
				t.Error("checkout persists credentials during adapter scripts")
			}
			if step.Name == "Commit packaged official update" && (step.Env["GITHUB_TOKEN"] == "" || !strings.Contains(step.Run, "extraheader")) {
				t.Error("trusted state push lacks explicit auth")
			}
		}
	}
}
