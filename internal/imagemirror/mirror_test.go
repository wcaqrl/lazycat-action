package imagemirror_test

import (
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/imagemirror"
)

func TestResolverUsesBuiltInDockerHubAndGHCRDefaults(t *testing.T) {
	resolver, err := imagemirror.FromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		source string
		want   string
	}{
		{source: "postgres", want: "docker.1ms.run/library/postgres:{tag}"},
		{source: "docker.io/calciumion/new-api", want: "docker.1ms.run/calciumion/new-api:{tag}"},
		{source: "ghcr.io/acme/web", want: "ghcr.1ms.run/acme/web:{tag}"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			got, err := resolver.Template(test.source, "")
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("template=%q want=%q", got, test.want)
			}
		})
	}
}

func TestResolverPreservesExistingTemplateWithoutEnvironmentOverride(t *testing.T) {
	resolver, err := imagemirror.FromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Template("ghcr.io/acme/web", "legacy.example/acme/web:{tag}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "legacy.example/acme/web:{tag}" {
		t.Fatalf("template=%q", got)
	}
}

func TestEnvironmentOverridesExistingTemplatesAndAddsRegistries(t *testing.T) {
	environment := map[string]string{
		imagemirror.EnvDockerMirror:    "docker-mirror.example/proxy",
		imagemirror.EnvGHCRMirror:      "ghcr-mirror.example",
		imagemirror.EnvRegistryMirrors: "quay.io=quay-mirror.example, registry.example.com=cn.example/registry",
	}
	resolver, err := imagemirror.FromEnvironment(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		source   string
		existing string
		want     string
	}{
		{source: "docker.io/acme/api", existing: "docker.1ms.run/acme/api:{tag}", want: "docker-mirror.example/proxy/acme/api:{tag}"},
		{source: "ghcr.io/acme/web", existing: "ghcr.1ms.run/acme/web:{tag}", want: "ghcr-mirror.example/acme/web:{tag}"},
		{source: "quay.io/acme/worker", want: "quay-mirror.example/acme/worker:{tag}"},
		{source: "registry.example.com/team/job", want: "cn.example/registry/team/job:{tag}"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			got, err := resolver.Template(test.source, test.existing)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("template=%q want=%q", got, test.want)
			}
		})
	}
}

func TestResolverExtractsUpstreamSourceFromKnownMirror(t *testing.T) {
	environment := map[string]string{
		imagemirror.EnvRegistryMirrors: "quay.io=cn.example/quay",
	}
	resolver, err := imagemirror.FromEnvironment(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		runtime string
		want    string
	}{
		{runtime: "docker.1ms.run/calciumion/new-api:v1.0.0", want: "docker.io/calciumion/new-api"},
		{runtime: "ghcr.1ms.run/acme/web:v2.3.4@sha256:" + strings.Repeat("a", 64), want: "ghcr.io/acme/web"},
		{runtime: "cn.example/quay/acme/worker:latest", want: "quay.io/acme/worker"},
	}
	for _, test := range tests {
		t.Run(test.runtime, func(t *testing.T) {
			got, found, err := resolver.ExtractSource(test.runtime)
			if err != nil {
				t.Fatal(err)
			}
			if !found || got != test.want {
				t.Fatalf("source=%q found=%t want=%q", got, found, test.want)
			}
		})
	}
}

func TestResolverRejectsInvalidEnvironment(t *testing.T) {
	tests := []map[string]string{
		{imagemirror.EnvDockerMirror: "https://mirror.example"},
		{imagemirror.EnvGHCRMirror: "mirror.example/proxy:latest"},
		{imagemirror.EnvRegistryMirrors: "quay.io"},
		{imagemirror.EnvRegistryMirrors: "quay.io=one.example,quay.io=two.example"},
		{imagemirror.EnvRegistryMirrors: "quay.io=docker.1ms.run"},
	}
	for _, environment := range tests {
		if _, err := imagemirror.FromEnvironment(func(name string) string { return environment[name] }); err == nil {
			t.Fatalf("environment=%#v", environment)
		}
	}
}

func TestResolverRejectsAmbiguousNestedMirrorExtraction(t *testing.T) {
	environment := map[string]string{
		imagemirror.EnvRegistryMirrors: "quay.io=docker.1ms.run/library",
	}
	resolver, err := imagemirror.FromEnvironment(func(name string) string { return environment[name] })
	if err != nil {
		t.Fatal(err)
	}
	_, found, err := resolver.ExtractSource("docker.1ms.run/library/postgres:v1")
	if err == nil || !strings.Contains(err.Error(), "multiple upstream repositories") || found {
		t.Fatalf("found=%t err=%v", found, err)
	}
}
