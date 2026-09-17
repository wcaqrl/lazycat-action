package delivery_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
	"github.com/wcaqrl/lazycat-action/internal/imagemirror"
	"github.com/wcaqrl/lazycat-action/internal/manifestedit"
	"github.com/wcaqrl/lazycat-action/internal/platform"
	"github.com/wcaqrl/lazycat-action/internal/registry"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

func TestLazyCatDeliveryCopiesAMD64AndReturnsCopyResult(t *testing.T) {
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage:  "ghcr.io/acme/web:v1.2.3",
		Platform:     "amd64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:v1.2.3",
		Progress:     appstore.CopyProgress{Finished: true, Layers: []appstore.LayerProgress{{Hash: "sha256:layer", Progress: 100}}},
	}}
	resolver := delivery.Resolver{Copier: copier}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if copier.request.Platform != "amd64" || copier.request.Image != "ghcr.io/acme/web:v1.2.3" {
		t.Fatalf("request=%#v", copier.request)
	}
	if result.RuntimeRef != "registry.lazycat.cloud/acme/web:v1.2.3" || !result.Copied || result.CopyResult == nil || !result.CopyResult.Progress.Finished {
		t.Fatalf("result=%#v", result)
	}
}

func TestDeliveryUsesConfiguredARM64Target(t *testing.T) {
	target := platform.Target{OS: "linux", Arch: "arm64"}
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage: "ghcr.io/acme/web:v1", Platform: "arm64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:v1", Progress: appstore.CopyProgress{Finished: true},
	}}
	resolver := delivery.Resolver{Copier: copier}
	if _, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}},
		Tag:   "v1", SourceRef: "ghcr.io/acme/web:v1", SourceDigest: digest("a"), Target: target,
	}); err != nil {
		t.Fatal(err)
	}
	if copier.request.Platform != "arm64" {
		t.Fatalf("copy request=%#v", copier.request)
	}

	inspector := &fakeInspector{result: registry.Image{Digest: digest("a"), Platform: "linux/arm64"}}
	resolver = delivery.Resolver{Inspector: inspector}
	if _, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "v1", SourceRef: "ghcr.io/acme/web:v1", SourceDigest: digest("a"), Target: target,
	}); err != nil {
		t.Fatal(err)
	}
	if inspector.target != target {
		t.Fatalf("inspect target=%#v", inspector.target)
	}
}

func TestDirectDeliveryUsesSourceWithoutExternalCalls(t *testing.T) {
	copier := &fakeCopier{err: errors.New("must not copy")}
	inspector := &fakeInspector{err: errors.New("must not inspect")}
	resolver := delivery.Resolver{Copier: copier, Inspector: inspector}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "direct"}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeRef != "ghcr.io/acme/web:v1.2.3" || result.Copied || copier.calls != 0 || inspector.calls != 0 {
		t.Fatalf("result=%#v copier=%d inspector=%d", result, copier.calls, inspector.calls)
	}
}

func TestMutableDirectDeliveryPinsDigestAndComparesCurrentState(t *testing.T) {
	result, err := (delivery.Resolver{}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "direct"}},
		Tag:   "latest", SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"),
		CurrentRef: "ghcr.io/acme/web:latest@" + digest("a"), Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeRef != "ghcr.io/acme/web:latest@"+digest("b") || result.CurrentDigest != digest("a") || !result.DigestChanged {
		t.Fatalf("result=%#v", result)
	}
}

func TestMutableLazyCatDeliverySkipsEqualDigestAndCopiesChangedDigest(t *testing.T) {
	currentRef := "registry.lazycat.cloud/acme/web:current"
	inspector := &fakeInspector{err: errors.New("must not inspect private LazyCat registry")}
	equalCopier := &fakeCopier{err: errors.New("must not copy")}
	result, err := (delivery.Resolver{Copier: equalCopier, Inspector: inspector}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("a"), CurrentRef: currentRef, CurrentDigest: digest("a"), Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DigestChanged || result.RuntimeRef != currentRef || equalCopier.calls != 0 {
		t.Fatalf("result=%#v copier=%d", result, equalCopier.calls)
	}
	if inspector.calls != 0 {
		t.Fatalf("private registry inspections=%d", inspector.calls)
	}

	changedCopier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage: "ghcr.io/acme/web:latest@" + digest("b"), Platform: "amd64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:new", Progress: appstore.CopyProgress{Finished: true},
	}}
	result, err = (delivery.Resolver{Copier: changedCopier, Inspector: inspector}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"), CurrentRef: currentRef, CurrentDigest: digest("a"), Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DigestChanged || result.CurrentDigest != digest("a") || !result.Copied || result.RuntimeRef != "registry.lazycat.cloud/acme/web:new" {
		t.Fatalf("result=%#v", result)
	}
	if changedCopier.request.Image != "ghcr.io/acme/web:latest@"+digest("b") {
		t.Fatalf("copy source=%q", changedCopier.request.Image)
	}
	if inspector.calls != 0 {
		t.Fatalf("private registry inspections=%d", inspector.calls)
	}
}

func TestMutableLazyCatDryRunUsesPersistedDigestWithoutCopying(t *testing.T) {
	copier := &fakeCopier{err: errors.New("must not copy")}
	result, err := (delivery.Resolver{Copier: copier}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"), CurrentRef: "registry.lazycat.cloud/acme/web:current", CurrentDigest: digest("a"), Mutable: true, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DigestChanged || copier.calls != 0 {
		t.Fatalf("result=%#v copier=%d", result, copier.calls)
	}
}

func TestMutableLazyCatMigratesEqualExternalImageWithoutBumpingDigest(t *testing.T) {
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage: "docker.io/acme/web:latest@" + digest("a"), Platform: "amd64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:migrated", Progress: appstore.CopyProgress{Finished: true},
	}}
	result, err := (delivery.Resolver{Copier: copier}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "docker.io/acme/web:latest", SourceDigest: digest("a"), CurrentRef: "docker.1ms.run/acme/web:latest", Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DigestChanged || !result.DeliveryChanged || !result.Copied || result.RuntimeRef != "registry.lazycat.cloud/acme/web:migrated" {
		t.Fatalf("result=%#v", result)
	}
}

func TestMutableLazyCatInitialBaselineComparesCopiedReference(t *testing.T) {
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage: "ghcr.io/acme/web:latest@" + digest("b"), Platform: "amd64",
		LazyCatImage: "registry.lazycat.cloud/acme/web:new", Progress: appstore.CopyProgress{Finished: true},
	}}
	result, err := (delivery.Resolver{Copier: copier}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"), CurrentRef: "registry.lazycat.cloud/acme/web:old", Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DigestChanged || !result.DeliveryChanged || !result.Copied {
		t.Fatalf("result=%#v", result)
	}
}

func TestMutableLazyCatInitialEqualReferenceStillPersistsBaseline(t *testing.T) {
	currentRef := "registry.lazycat.cloud/acme/web:current"
	copier := &fakeCopier{result: appstore.CopyImageResult{
		SourceImage: "ghcr.io/acme/web:latest@" + digest("a"), Platform: "amd64",
		LazyCatImage: currentRef, Progress: appstore.CopyProgress{Finished: true},
	}}
	result, err := (delivery.Resolver{Copier: copier}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("a"), CurrentRef: currentRef, Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DigestChanged || !result.DeliveryChanged || !result.Copied || result.RuntimeRef != currentRef {
		t.Fatalf("result=%#v", result)
	}
}

func TestMutableLazyCatDryRunRejectsMissingPrivateBaseline(t *testing.T) {
	_, err := (delivery.Resolver{}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}}, Tag: "latest",
		SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"), CurrentRef: "registry.lazycat.cloud/acme/web:old", Mutable: true, DryRun: true,
	})
	if err == nil || !strings.Contains(err.Error(), "baseline is missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestLazyCatDeliveryRejectsMismatchedCopyResult(t *testing.T) {
	tests := []struct {
		name   string
		result appstore.CopyImageResult
	}{
		{name: "source", result: appstore.CopyImageResult{SourceImage: "ghcr.io/acme/other:v1", Platform: "amd64", LazyCatImage: "registry.lazycat.cloud/acme/web:v1", Progress: appstore.CopyProgress{Finished: true}}},
		{name: "platform", result: appstore.CopyImageResult{SourceImage: "ghcr.io/acme/web:v1", Platform: "arm64", LazyCatImage: "registry.lazycat.cloud/acme/web:v1", Progress: appstore.CopyProgress{Finished: true}}},
		{name: "registry", result: appstore.CopyImageResult{SourceImage: "ghcr.io/acme/web:v1", Platform: "amd64", LazyCatImage: "ghcr.io/acme/web:v1", Progress: appstore.CopyProgress{Finished: true}}},
		{name: "unfinished", result: appstore.CopyImageResult{SourceImage: "ghcr.io/acme/web:v1", Platform: "amd64", LazyCatImage: "registry.lazycat.cloud/acme/web:v1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := delivery.Resolver{Copier: &fakeCopier{result: test.result}}
			_, err := resolver.Deliver(context.Background(), delivery.Request{
				Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat"}},
				Tag:   "v1", SourceRef: "ghcr.io/acme/web:v1", SourceDigest: digest("a"),
			})
			if err == nil || !strings.Contains(err.Error(), "invalid LazyCat copy result") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestMirrorDeliveryExpandsTemplateAndVerifiesDigest(t *testing.T) {
	inspector := &fakeInspector{result: registry.Image{Digest: digest("a"), Platform: "linux/amd64"}}
	resolver := delivery.Resolver{Inspector: inspector}
	result, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "ghcr.1ms.run/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeRef != "ghcr.1ms.run/acme/web:v1.2.3" || inspector.reference != result.RuntimeRef || result.Copied {
		t.Fatalf("result=%#v inspector=%#v", result, inspector)
	}
}

func TestMirrorDeliveryUsesBuiltInTemplateAndEnvironmentOverride(t *testing.T) {
	for _, test := range []struct {
		name        string
		environment map[string]string
		existing    string
		want        string
	}{
		{name: "built-in", want: "ghcr.1ms.run/acme/web:v1.2.3"},
		{name: "override", environment: map[string]string{imagemirror.EnvGHCRMirror: "mirror.example/ghcr"}, existing: "legacy.example/acme/web:{tag}", want: "mirror.example/ghcr/acme/web:v1.2.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mirrors, err := imagemirror.FromEnvironment(func(name string) string { return test.environment[name] })
			if err != nil {
				t.Fatal(err)
			}
			result, err := (delivery.Resolver{Mirrors: mirrors}).Deliver(context.Background(), delivery.Request{
				Image: config.Image{ID: "web", Source: "ghcr.io/acme/web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: test.existing}},
				Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.RuntimeRef != test.want {
				t.Fatalf("runtime=%q want=%q", result.RuntimeRef, test.want)
			}
		})
	}
}

func TestResolverRecoversMirrorSourceFromManifest(t *testing.T) {
	mirrors, err := imagemirror.FromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	resolver := delivery.Resolver{Mirrors: mirrors}
	for _, current := range []manifestedit.Current{
		{UpstreamRef: "ghcr.io/acme/web:v1.2.3@" + digest("a"), RuntimeRef: "unrecognized.example/web:v1"},
		{RuntimeRef: "ghcr.1ms.run/acme/web:v1.2.3"},
	} {
		resolved, err := resolver.ResolveImage(config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror"}}, current)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Source != "ghcr.io/acme/web" || resolved.Delivery.ImageTemplate != "ghcr.1ms.run/acme/web:{tag}" {
			t.Fatalf("resolved=%#v", resolved)
		}
	}
}

func TestMirrorDeliveryRejectsDigestMismatch(t *testing.T) {
	resolver := delivery.Resolver{Inspector: &fakeInspector{result: registry.Image{Digest: digest("b"), Platform: "linux/amd64"}}}
	_, err := resolver.Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "v1.2.3", SourceRef: "ghcr.io/acme/web:v1.2.3", SourceDigest: digest("a"),
	})
	if err == nil || !errors.Is(err, delivery.ErrMirrorVerification) || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("err=%v", err)
	}
}

func TestMutableMirrorDeliveryUsesPinnedPreviousDigest(t *testing.T) {
	inspector := &sequenceInspector{results: []registry.Image{
		{Digest: digest("a"), Platform: "linux/amd64"},
		{Digest: digest("b"), Platform: "linux/amd64"},
	}}
	result, err := (delivery.Resolver{Inspector: inspector}).Deliver(context.Background(), delivery.Request{
		Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
		Tag:   "latest", SourceRef: "ghcr.io/acme/web:latest", SourceDigest: digest("b"),
		CurrentRef: "mirror/acme/web:latest@" + digest("a"), Mutable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DigestChanged || result.CurrentDigest != digest("a") || result.RuntimeRef != "mirror/acme/web:latest@"+digest("b") {
		t.Fatalf("result=%#v", result)
	}
}

func TestDryRunDoesNotCopyOrInspect(t *testing.T) {
	resolver := delivery.Resolver{Copier: &fakeCopier{err: errors.New("must not copy")}, Inspector: &fakeInspector{err: errors.New("must not inspect")}}
	for _, image := range []config.Image{
		{ID: "lazycat", Delivery: config.Delivery{Mode: "lazycat"}},
		{ID: "mirror", Delivery: config.Delivery{Mode: "mirror", ImageTemplate: "mirror/acme/web:{tag}", RequireDigestMatch: true}},
	} {
		result, err := resolver.Deliver(context.Background(), delivery.Request{Image: image, Tag: "v1", SourceRef: "source:v1", SourceDigest: digest("a"), DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.Copied {
			t.Fatalf("result=%#v", result)
		}
	}
}

type fakeCopier struct {
	request appstore.CopyImageRequest
	result  appstore.CopyImageResult
	err     error
	calls   int
}

func (copier *fakeCopier) CopyImage(_ context.Context, request appstore.CopyImageRequest) (appstore.CopyImageResult, error) {
	copier.calls++
	copier.request = request
	return copier.result, copier.err
}

type fakeInspector struct {
	reference string
	result    registry.Image
	err       error
	calls     int
	target    platform.Target
}

type sequenceInspector struct {
	results []registry.Image
	calls   int
}

func (inspector *sequenceInspector) InspectTarget(_ context.Context, _ string, _ platform.Target) (registry.Image, error) {
	if inspector.calls >= len(inspector.results) {
		return registry.Image{}, errors.New("unexpected inspection")
	}
	result := inspector.results[inspector.calls]
	inspector.calls++
	return result, nil
}

func (inspector *sequenceInspector) Inspect(_ context.Context, _ string) (registry.Image, error) {
	return inspector.InspectTarget(context.Background(), "", platform.Target{})
}

func (inspector *fakeInspector) InspectTarget(_ context.Context, reference string, target platform.Target) (registry.Image, error) {
	inspector.calls++
	inspector.reference = reference
	inspector.target = target
	return inspector.result, inspector.err
}

func (inspector *fakeInspector) Inspect(_ context.Context, reference string) (registry.Image, error) {
	inspector.calls++
	inspector.reference = reference
	return inspector.result, inspector.err
}

func digest(character string) string { return "sha256:" + strings.Repeat(character, 64) }
