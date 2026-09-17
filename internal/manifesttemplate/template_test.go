package manifesttemplate_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/manifesttemplate"
	"go.yaml.in/yaml/v3"
)

func TestProtectMakesStandaloneControlsValidYAML(t *testing.T) {
	source := []byte(`application:
  subdomain: templated
{{- if .U.multi_instance }}
  multi_instance: true
{{- else }}
  multi_instance: false
{{- end }}
services:
  app:
    image: registry.lazycat.cloud/example/app:old
    environment:
      - PASSWORD={{.U.password}}
`)

	protected, err := manifesttemplate.Protect(source)
	if err != nil {
		t.Fatal(err)
	}
	encoded := protected.Bytes()
	var node yaml.Node
	if err := yaml.Unmarshal(encoded, &node); err != nil {
		t.Fatalf("protected YAML is invalid: %v\n%s", err, encoded)
	}
	for _, control := range [][]byte{
		[]byte("{{- if .U.multi_instance }}"),
		[]byte("{{- else }}"),
		[]byte("{{- end }}"),
	} {
		if bytes.Contains(encoded, control) {
			t.Fatalf("protected YAML still contains standalone control %q:\n%s", control, encoded)
		}
	}
	if !bytes.Contains(encoded, []byte("PASSWORD={{.U.password}}")) {
		t.Fatalf("inline expression was changed:\n%s", encoded)
	}
}

func TestRestorePreservesExactControlLinesAfterYAMLRoundTrip(t *testing.T) {
	source := []byte(`application:
  subdomain: templated
  {{- if .U.multi_instance -}}
  multi_instance: true
  {{- else -}}
  multi_instance: false
  {{- end -}}
`)
	protected, err := manifesttemplate.Protect(source)
	if err != nil {
		t.Fatal(err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(protected.Bytes(), &node); err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(&node)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := protected.Restore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range [][]byte{
		[]byte("  {{- if .U.multi_instance -}}"),
		[]byte("  {{- else -}}"),
		[]byte("  {{- end -}}"),
	} {
		found := false
		for _, line := range bytes.Split(restored, []byte("\n")) {
			if bytes.Equal(line, control) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("restored YAML missing exact control %q:\n%s", control, restored)
		}
	}
}

func TestProtectRecognizesWithAndRangeControls(t *testing.T) {
	source := []byte(`settings:
{{ with .U.settings }}
  enabled: true
{{ end }}
items:
{{- range .U.items }}
  - name: item
{{- end }}
`)
	protected, err := manifesttemplate.Protect(source)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(protected.Bytes(), []byte("{{ with")) || bytes.Contains(protected.Bytes(), []byte("{{- range")) {
		t.Fatalf("with or range control was not protected:\n%s", protected.Bytes())
	}
	var node yaml.Node
	if err := yaml.Unmarshal(protected.Bytes(), &node); err != nil {
		t.Fatalf("protected YAML is invalid: %v\n%s", err, protected.Bytes())
	}
	restored, err := protected.Restore(protected.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, source) {
		t.Fatalf("restored YAML differs:\n%s", restored)
	}
}

func TestPlainYAMLRoundTripsWithoutControls(t *testing.T) {
	source := []byte("application:\n  subdomain: plain\n")
	protected, err := manifesttemplate.Protect(source)
	if err != nil {
		t.Fatal(err)
	}
	first := protected.Bytes()
	if !bytes.Equal(first, source) {
		t.Fatalf("protected plain YAML differs: %q", first)
	}
	first[0] = 'X'
	if bytes.Equal(protected.Bytes(), first) {
		t.Fatal("Bytes returned mutable internal data")
	}

	encoded := []byte("application:\n  subdomain: encoded\n")
	restored, err := protected.Restore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, encoded) {
		t.Fatalf("restored plain YAML differs: %q", restored)
	}
	restored[0] = 'X'
	if encoded[0] == 'X' {
		t.Fatal("Restore returned the caller's input buffer")
	}
}

func TestProtectRejectsReservedMarkerPrefix(t *testing.T) {
	source := []byte("# lazycat-action-template-control-user-content\napplication: {}\n")
	if _, err := manifesttemplate.Protect(source); err == nil {
		t.Fatal("expected reserved marker prefix to be rejected")
	}
}

func TestProtectRejectsYAMLThatRemainsInvalidAfterProtection(t *testing.T) {
	source := []byte("{{ if .U.enabled }}\napplication:\n  valid: true\n invalid: false\n{{ end }}\n")
	if _, err := manifesttemplate.Protect(source); err == nil {
		t.Fatal("expected invalid protected YAML to be rejected")
	} else if !strings.Contains(err.Error(), "validating protected manifest YAML") {
		t.Fatalf("error lacks validation context: %v", err)
	}
}

func TestRestoreRejectsMissingMarker(t *testing.T) {
	protected, err := manifesttemplate.Protect([]byte("{{ if .U.enabled }}\nenabled: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	encoded := bytes.Replace(protected.Bytes(), []byte("# lazycat-action-template-control-0"), []byte("# marker removed"), 1)
	if _, err := protected.Restore(encoded); err == nil {
		t.Fatal("expected missing marker to be rejected")
	}
}

func TestRestoreRejectsDuplicatedMarker(t *testing.T) {
	protected, err := manifesttemplate.Protect([]byte("{{ if .U.enabled }}\nenabled: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("# lazycat-action-template-control-0")
	encoded := append(protected.Bytes(), append(marker, '\n')...)
	if _, err := protected.Restore(encoded); err == nil {
		t.Fatal("expected duplicated marker to be rejected")
	}
}

func TestProtectLeavesNonControlInlineExpressionsUntouched(t *testing.T) {
	source := []byte("environment:\n  - PASSWORD={{.U.password}}\nvalue: '{{ .U.value }}'\n")
	protected, err := manifesttemplate.Protect(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(protected.Bytes(), source) {
		t.Fatalf("inline expressions changed:\n%s", protected.Bytes())
	}
	restored, err := protected.Restore(protected.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, source) {
		t.Fatalf("inline expressions changed during restore:\n%s", restored)
	}
}
