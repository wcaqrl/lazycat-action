package official_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/lib-x/lzc-toolkit-go/lpk"
)

func TestPublisherRetryDefaultsOffAfterOneCompleteAttempt(t *testing.T) {
	path, digest := publishLPK(t)
	checks := 0
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			checks++
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: false, MaxAttempts: 3, InitialDelay: 2 * time.Second, MaxDelay: 30 * time.Second},
	})
	if err == nil || checks != 1 || uploads != 1 {
		t.Fatalf("err=%v checks=%d uploads=%d", err, checks, uploads)
	}
}

func TestPublisherAutomaticallySubmitsMissingApplicationInformation(t *testing.T) {
	path, digest := publishLPK(t)
	root := t.TempDir()
	writeApplicationScreenshot(t, filepath.Join(root, "pc-one.png"), color.RGBA{R: 220, A: 255})
	writeApplicationScreenshot(t, filepath.Join(root, "pc-two.png"), color.RGBA{B: 220, A: 255})
	writeApplicationScreenshot(t, filepath.Join(root, "mobile-one.png"), color.RGBA{G: 180, A: 255})
	writeApplicationScreenshot(t, filepath.Join(root, "mobile-two.png"), color.RGBA{R: 120, G: 120, A: 255})
	writeApplicationScreenshot(t, filepath.Join(root, "mobile-three.png"), color.RGBA{B: 120, G: 120, A: 255})
	uploadedImages := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/list":
			if request.URL.Query().Get("seek") != "cloud.lazycat.apps.publish-demo" {
				t.Fatalf("query=%s", request.URL.RawQuery)
			}
			_, _ = response.Write([]byte(`{"items":[{"id":7,"package":"cloud.lazycat.apps.publish-demo","waiting_review_id":null,"resource":{"info_data":{}}}]}`))
		case "/api/v3/developer/upload":
			uploadedImages++
			if err := request.ParseMultipartForm(4 << 20); err != nil {
				t.Fatal(err)
			}
			file, header, err := request.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			configuration, format, decodeErr := image.DecodeConfig(file)
			closeErr := file.Close()
			if decodeErr != nil || closeErr != nil || format != "png" || configuration.Width != 640 || configuration.Height != 360 || !strings.HasSuffix(header.Filename, ".png") {
				t.Fatalf("filename=%q image=%#v format=%q decodeErr=%v closeErr=%v", header.Filename, configuration, format, decodeErr, closeErr)
			}
			_, _ = fmt.Fprintf(response, `{"url":"/screens/image-%d.png"}`, uploadedImages)
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			var body struct {
				Infos []struct {
					Language              string   `json:"language"`
					Brief                 string   `json:"brief"`
					SupportPC             bool     `json:"support_pc"`
					SupportMobile         bool     `json:"support_mobile"`
					ScreenshotPCPaths     []string `json:"screenshot_pc_paths"`
					ScreenshotMobilePaths []string `json:"screenshot_mobile_paths"`
				} `json:"infos"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Infos) != 1 || body.Infos[0].Language != "zh" || body.Infos[0].Brief != "Collaborative workspace" || !body.Infos[0].SupportPC || !body.Infos[0].SupportMobile || strings.Join(body.Infos[0].ScreenshotPCPaths, ",") != "/screens/image-1.png,/screens/image-2.png" || strings.Join(body.Infos[0].ScreenshotMobilePaths, ",") != "/screens/image-3.png,/screens/image-4.png,/screens/image-5.png" {
				t.Fatalf("infos=%#v", body.Infos)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), ProjectRoot: root, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"zh"}, CreateIfMissing: true,
		Application: config.OfficialApplication{
			Language: "zh", Name: "Publish Demo", Brief: "Collaborative workspace", SupportPC: true, SupportMobile: true,
			ScreenshotPCFiles:     []string{"pc-one.png", "pc-two.png"},
			ScreenshotMobileFiles: []string{"mobile-one.png", "mobile-two.png", "mobile-three.png"},
		},
	})
	if err != nil || !result.Published || uploadedImages != 5 {
		t.Fatalf("result=%#v uploadedImages=%d err=%v", result, uploadedImages, err)
	}
}

func TestPublisherCreatesMissingApplicationWithFirstInformationReview(t *testing.T) {
	path, digest := publishLPK(t)
	root := t.TempDir()
	writeApplicationScreenshot(t, filepath.Join(root, "pc-one.png"), color.RGBA{R: 220, A: 255})
	writeApplicationScreenshot(t, filepath.Join(root, "pc-two.png"), color.RGBA{B: 220, A: 255})
	created, uploadedImages := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/list":
			_, _ = response.Write([]byte(`{"items":[]}`))
		case "/api/v3/developer/app/create":
			created++
			var body appstore.CreateApplicationRequest
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Package != "cloud.lazycat.apps.publish-demo" || body.Name != "Publish Demo" {
				t.Fatalf("application=%#v", body)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		case "/api/v3/developer/upload":
			uploadedImages++
			_, _ = fmt.Fprintf(response, `{"url":"/screens/image-%d.png"}`, uploadedImages)
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			var body struct {
				Infos []appstore.ApplicationInfo `json:"infos"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Infos) != 1 || len(body.Infos[0].ScreenshotPCPaths) != 2 {
				t.Fatalf("infos=%#v", body.Infos)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), ProjectRoot: root, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"zh"}, CreateIfMissing: true,
		Application: config.OfficialApplication{
			Language: "zh", Name: "Publish Demo", Brief: "Workspace", SupportPC: true,
			ScreenshotPCFiles: []string{"pc-one.png", "pc-two.png"},
		},
	})
	if err != nil || !result.Published || !result.Created || created != 1 || uploadedImages != 2 {
		t.Fatalf("result=%#v created=%d uploadedImages=%d err=%v", result, created, uploadedImages, err)
	}
}

func TestPublisherDoesNotResubmitApprovedApplicationInformation(t *testing.T) {
	path, digest := publishLPK(t)
	listRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/list":
			listRequests++
			_, _ = response.Write([]byte(`{"items":[{"id":7,"package":"cloud.lazycat.apps.publish-demo","resource":{"info_data":{"zh":{"language":"zh","name":"Publish Demo","brief":"Workspace","support_pc":true,"screenshot_pc_paths":["/one.png","/two.png"]}}}}]}`))
		case "/api/v3/developer/upload":
			t.Fatal("approved application information must not upload screenshots")
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			var body map[string]json.RawMessage
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, found := body["infos"]; found {
				t.Fatalf("unexpected infos=%s", body["infos"])
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), ProjectRoot: t.TempDir(), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"zh"}, CreateIfMissing: true,
		Application: config.OfficialApplication{
			Language: "zh", Name: "Publish Demo", Brief: "Workspace", SupportPC: true,
			ScreenshotPCFiles: []string{"missing-one.png", "missing-two.png"},
		},
	})
	if err != nil || !result.Published || listRequests != 2 {
		t.Fatalf("result=%#v listRequests=%d err=%v", result, listRequests, err)
	}
}

func TestPublisherStopsWhenApplicationReviewIsAlreadyPending(t *testing.T) {
	path, digest := publishLPK(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/api/v3/developer/app/list" {
			t.Fatalf("unexpected request=%s", request.URL.Path)
		}
		_, _ = response.Write([]byte(`{"items":[{"id":7,"package":"cloud.lazycat.apps.publish-demo","waiting_review_id":19,"resource":{"info_data":{}}}]}`))
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), ProjectRoot: t.TempDir(), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"zh"}, CreateIfMissing: true,
		Application: config.OfficialApplication{
			Language: "zh", Name: "Publish Demo", Brief: "Workspace", SupportPC: true,
			ScreenshotPCFiles: []string{"missing-one.png", "missing-two.png"},
		},
	})
	if !errors.Is(err, lpkgo.ErrConflict) || requests != 1 {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
}

func TestPublisherReturnsSafeScreenshotPathAndDetail(t *testing.T) {
	path, digest := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v3/developer/app/list" {
			t.Fatalf("unexpected request=%s", request.URL.Path)
		}
		_, _ = response.Write([]byte(`{"items":[{"id":7,"package":"cloud.lazycat.apps.publish-demo","resource":{"info_data":{}}}]}`))
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), ProjectRoot: t.TempDir(), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"zh"}, CreateIfMissing: true,
		Application: config.OfficialApplication{
			Language: "zh", Name: "Publish Demo", Brief: "Workspace", SupportPC: true,
			ScreenshotPCFiles: []string{"missing-one.png", "missing-two.png"},
		},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeInvalidConfig || toolkitError.Op != "store.official.screenshot" {
		t.Fatalf("err=%v", err)
	}
	pathDetail, pathOK := toolkitError.Cause.(interface{ PublicErrorPath() string })
	messageDetail, messageOK := toolkitError.Cause.(interface{ PublicErrorDetail() string })
	if !pathOK || !messageOK || pathDetail.PublicErrorPath() != "missing-one.png" || messageDetail.PublicErrorDetail() != "screenshot file does not exist" {
		t.Fatalf("cause=%#v", toolkitError.Cause)
	}
}

func TestPublisherRetryDisabledIgnoresDelayValuesAndCallbacks(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			panic("disabled retry initialized delay policy")
		},
		Wait: func(context.Context, time.Duration) error {
			panic("disabled retry waited")
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: false, MaxAttempts: -1, InitialDelay: -time.Second, MaxDelay: -time.Second},
	})
	if err == nil || uploads != 1 {
		t.Fatalf("err=%v uploads=%d", err, uploads)
	}
}

func TestPublisherRetryRepeatsCompleteAttemptAfterTransientUploadFailure(t *testing.T) {
	path, digest := publishLPK(t)
	checks := 0
	uploads := 0
	reviews := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			checks++
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			if uploads == 1 {
				http.Error(response, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviews++
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	publisher := official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}
	result, err := publisher.Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil || !result.Published || checks != 2 || uploads != 2 || reviews != 1 {
		t.Fatalf("err=%v result=%#v checks=%d uploads=%d reviews=%d", err, result, checks, uploads, reviews)
	}
}

func TestPublisherRetryPreservesApplicationCreatedAcrossAttempts(t *testing.T) {
	path, digest := publishLPK(t)
	checks := 0
	creates := 0
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			checks++
			_, _ = fmt.Fprintf(response, `{"exist":%t}`, checks > 1)
		case "/api/v3/developer/app/create":
			creates++
			_, _ = response.Write([]byte(`{"success":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			if uploads == 1 {
				http.Error(response, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"}, CreateIfMissing: true,
		Application: config.OfficialApplication{Language: "en", Name: "Publish Demo"},
		Retry:       config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil || !result.Created || checks != 2 || creates != 1 || uploads != 2 {
		t.Fatalf("err=%v result=%#v checks=%d creates=%d uploads=%d", err, result, checks, creates, uploads)
	}
}

func TestPublisherRetryDoesNotRepeatHTTP400(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "bad request", http.StatusBadRequest)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.StatusCode != http.StatusBadRequest || uploads != 1 {
		t.Fatalf("err=%v uploads=%d", err, uploads)
	}
}

func TestPublisherRetryExhaustionReturnsFinalErrorAndExactAttemptCount(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	waits := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return time.Millisecond }
		},
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Op != "store.official.upload" || toolkitError.StatusCode != http.StatusServiceUnavailable || !toolkitError.Retryable || uploads != 3 || waits != 2 {
		t.Fatalf("err=%#v uploads=%d waits=%d", toolkitError, uploads, waits)
	}
}

func TestPublisherRetryResolvesTokenOnceAcrossAttempts(t *testing.T) {
	path, digest := publishLPK(t)
	provider := &countingTokenProvider{token: "ci-token"}
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, _ = (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}).Publish(context.Background(), official.Request{
		Provider: provider, LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if provider.calls != 1 || uploads != 3 {
		t.Fatalf("token calls=%d uploads=%d", provider.calls, uploads)
	}
}

func TestPublisherRetryCancellationDuringWaitStopsBeforeNextAttempt(t *testing.T) {
	path, digest := publishLPK(t)
	checks := 0
	uploads := 0
	waiting := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			checks++
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (official.Publisher{
			BaseURL: server.URL, HTTPClient: server.Client(),
			NewDelay: func(_, _ time.Duration) func() time.Duration {
				return func() time.Duration { return time.Hour }
			},
			Wait: func(ctx context.Context, _ time.Duration) error {
				close(waiting)
				<-ctx.Done()
				return ctx.Err()
			},
		}).Publish(ctx, official.Request{
			Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
			Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
			Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
		})
		done <- err
	}()
	<-waiting
	cancel()
	select {
	case err := <-done:
		var toolkitError *lpkgo.Error
		if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeCancelled || checks != 1 || uploads != 1 {
			t.Fatalf("err=%v checks=%d uploads=%d", err, checks, uploads)
		}
	case <-time.After(time.Second):
		t.Fatal("publish did not return promptly after retry wait cancellation")
	}
}

func TestPublisherRetryDeadlineDuringWaitPreservesDeadlineCode(t *testing.T) {
	path, digest := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			http.Error(response, "unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return time.Second }
		},
		Wait: func(context.Context, time.Duration) error {
			return context.DeadlineExceeded
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeDeadlineExceeded {
		t.Fatalf("err=%v", err)
	}
}

func TestPublisherRetryAfterLargerDelayIsSelectedAndCapped(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	var waited time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			if uploads == 1 {
				response.Header().Set("Retry-After", "60")
				http.Error(response, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 2 * time.Second }
		},
		Wait: func(_ context.Context, delay time.Duration) error {
			waited = delay
			return nil
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil || waited != 10*time.Second {
		t.Fatalf("err=%v waited=%s", err, waited)
	}
}

func TestPublisherRetryJitterDelayWinsOverSmallerRetryAfter(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	var waited time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			if uploads == 1 {
				response.Header().Set("Retry-After", "1")
				http.Error(response, "unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration { return func() time.Duration { return 7 * time.Second } },
		Wait: func(_ context.Context, delay time.Duration) error {
			waited = delay
			return nil
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil || waited != 7*time.Second {
		t.Fatalf("err=%v waited=%s", err, waited)
	}
}

func TestPublisherRetryWarningContainsOnlySafeStructuredFields(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			if uploads == 1 {
				http.Error(response, "server-secret", http.StatusServiceUnavailable)
				return
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 2 * time.Second }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("token-secret"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"}, Logger: logger,
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.NewDecoder(&logs).Decode(&record); err != nil {
		t.Fatalf("decode warning: %v; logs=%q", err, logs.String())
	}
	allowed := map[string]bool{
		"time": true, "level": true, "msg": true, "store": true, "attempt": true,
		"max_attempts": true, "delay": true, "code": true, "status": true,
	}
	for key := range record {
		if !allowed[key] {
			t.Fatalf("unsafe warning field %q in %#v", key, record)
		}
	}
	if len(record) != len(allowed) || record["level"] != "WARN" || record["msg"] != "official store publication retry scheduled" || record["store"] != "official" || record["attempt"] != float64(1) || record["max_attempts"] != float64(2) || record["delay"] != float64(2*time.Second) || record["code"] != string(lpkgo.CodeRemoteUnavailable) || record["status"] != float64(http.StatusServiceUnavailable) {
		t.Fatalf("warning=%#v", record)
	}
	if strings.Contains(logs.String(), "token-secret") || strings.Contains(logs.String(), "server-secret") {
		t.Fatalf("warning leaked secret: %s", logs.String())
	}
}

func TestPublisherAttemptMarksTransientUploadAndReviewErrorsRetryable(t *testing.T) {
	for _, test := range []struct {
		name   string
		stage  string
		status int
	}{
		{name: "upload 429", stage: "upload", status: http.StatusTooManyRequests},
		{name: "upload 500", stage: "upload", status: http.StatusInternalServerError},
		{name: "review 429", stage: "review", status: http.StatusTooManyRequests},
		{name: "review 500", stage: "review", status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, digest := publishLPK(t)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v3/developer/app/check/exist":
					_, _ = response.Write([]byte(`{"exist":true}`))
				case "/api/v3/developer/app/lpk/upload":
					if test.stage == "upload" {
						http.Error(response, "transient", test.status)
						return
					}
					_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
				case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
					http.Error(response, "transient", test.status)
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
				Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
				Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
			})
			var toolkitError *lpkgo.Error
			if !errors.As(err, &toolkitError) || toolkitError.Op != "store.official."+test.stage || toolkitError.StatusCode != test.status || !toolkitError.Retryable {
				t.Fatalf("err=%#v", toolkitError)
			}
		})
	}
}

func TestPublisherRetryDoesNotReplayAmbiguousReviewFailure(t *testing.T) {
	path, digest := publishLPK(t)
	checks := 0
	uploads := 0
	reviews := 0
	waits := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			checks++
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviews++
			http.Error(response, "accepted but response failed", http.StatusInternalServerError)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error {
			waits++
			return nil
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Op != "store.official.review" || toolkitError.StatusCode != http.StatusInternalServerError || checks != 1 || uploads != 1 || reviews != 1 || waits != 0 {
		t.Fatalf("err=%#v checks=%d uploads=%d reviews=%d waits=%d", toolkitError, checks, uploads, reviews, waits)
	}
}

func TestPublisherRetryDoesNotRepeatStatus600(t *testing.T) {
	path, digest := publishLPK(t)
	uploads := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploads++
			response.WriteHeader(600)
			_, _ = response.Write([]byte("not an HTTP 5xx response"))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: server.Client(),
		NewDelay: func(_, _ time.Duration) func() time.Duration { return func() time.Duration { return 0 } },
		Wait: func(context.Context, time.Duration) error {
			panic("status 600 waited")
		},
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.StatusCode != 600 || toolkitError.Retryable || uploads != 1 {
		t.Fatalf("err=%#v uploads=%d", toolkitError, uploads)
	}
}

func TestPublisherRetryDoesNotRepeatUnreadable4xxResponse(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		body     func() io.ReadCloser
		wantCode lpkgo.Code
	}{
		{name: "truncated 401", status: http.StatusUnauthorized, body: func() io.ReadCloser { return io.NopCloser(errorReader{}) }, wantCode: lpkgo.CodeUnauthenticated},
		{name: "truncated 403", status: http.StatusForbidden, body: func() io.ReadCloser { return io.NopCloser(errorReader{}) }, wantCode: lpkgo.CodePermissionDenied},
		{name: "truncated 404", status: http.StatusNotFound, body: func() io.ReadCloser { return io.NopCloser(errorReader{}) }, wantCode: lpkgo.CodeRemoteUnavailable},
		{name: "truncated other 4xx", status: http.StatusTeapot, body: func() io.ReadCloser { return io.NopCloser(errorReader{}) }, wantCode: lpkgo.CodeRemoteUnavailable},
		{name: "oversized 404", status: http.StatusNotFound, body: func() io.ReadCloser { return io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1))) }, wantCode: lpkgo.CodeRemoteUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, digest := publishLPK(t)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v3/developer/app/check/exist":
					_, _ = response.Write([]byte(`{"exist":true}`))
				case "/api/v3/developer/app/lpk/upload":
					_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			httpClient := server.Client()
			baseTransport := httpClient.Transport
			responses := 0
			httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/review/create") {
					responses++
					return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: test.body(), Request: request}, nil
				}
				return baseTransport.RoundTrip(request)
			})
			_, err := (official.Publisher{
				BaseURL: server.URL, HTTPClient: httpClient,
				NewDelay: func(_, _ time.Duration) func() time.Duration { return func() time.Duration { return 0 } },
				Wait: func(context.Context, time.Duration) error {
					panic("non-retryable 4xx response waited")
				},
			}).Publish(context.Background(), official.Request{
				Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
				Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
				Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
			})
			var toolkitError *lpkgo.Error
			if !errors.As(err, &toolkitError) || toolkitError.Code != test.wantCode || toolkitError.StatusCode != test.status || toolkitError.Retryable || responses != 1 {
				t.Fatalf("err=%#v responses=%d", toolkitError, responses)
			}
		})
	}
}

func TestPublisherRetryStopsForContextErrorDuringResponseRead(t *testing.T) {
	for _, test := range []struct {
		name     string
		readErr  error
		wantCode lpkgo.Code
	}{
		{name: "cancelled", readErr: context.Canceled, wantCode: lpkgo.CodeCancelled},
		{name: "deadline", readErr: context.DeadlineExceeded, wantCode: lpkgo.CodeDeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, digest := publishLPK(t)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v3/developer/app/check/exist":
					_, _ = response.Write([]byte(`{"exist":true}`))
				case "/api/v3/developer/app/lpk/upload":
					_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			ctx := newManualContext()
			httpClient := server.Client()
			baseTransport := httpClient.Transport
			reads := 0
			httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/review/create") {
					return &http.Response{
						StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
						Body: readCloserFunc(func([]byte) (int, error) {
							reads++
							ctx.finish(test.readErr)
							return 0, test.readErr
						}),
					}, nil
				}
				return baseTransport.RoundTrip(request)
			})
			_, err := (official.Publisher{
				BaseURL: server.URL, HTTPClient: httpClient,
				NewDelay: func(_, _ time.Duration) func() time.Duration { return func() time.Duration { return 0 } },
				Wait: func(context.Context, time.Duration) error {
					panic("context response read error waited")
				},
			}).Publish(ctx, official.Request{
				Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
				Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
				Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
			})
			var toolkitError *lpkgo.Error
			if !errors.As(err, &toolkitError) || toolkitError.Code != test.wantCode || toolkitError.Op != "store.official.review" || toolkitError.StatusCode != http.StatusOK || reads != 1 {
				t.Fatalf("err=%#v reads=%d", toolkitError, reads)
			}
		})
	}
}

func TestPublisherRetryDoesNotRepeatInvalidUploadMetadata(t *testing.T) {
	for _, test := range []struct {
		name     string
		response func(string) string
	}{
		{
			name: "mismatched identity",
			response: func(string) string {
				return `{"package":"cloud.lazycat.apps.publish-demo","version":"9.9.9","url":"/demo.lpk","sha256":"bad"}`
			},
		},
		{
			name:     "invalid JSON",
			response: func(string) string { return `{` },
		},
		{
			name: "incomplete metadata",
			response: func(digest string) string {
				return fmt.Sprintf(`{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","sha256":"%s"}`, digest)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, digest := publishLPK(t)
			uploads := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v3/developer/app/check/exist":
					_, _ = response.Write([]byte(`{"exist":true}`))
				case "/api/v3/developer/app/lpk/upload":
					uploads++
					_, _ = response.Write([]byte(test.response(digest)))
				default:
					http.NotFound(response, request)
				}
			}))
			defer server.Close()

			_, err := (official.Publisher{
				BaseURL: server.URL, HTTPClient: server.Client(),
				NewDelay: func(_, _ time.Duration) func() time.Duration {
					return func() time.Duration { return 0 }
				},
				Wait: func(context.Context, time.Duration) error { return nil },
			}).Publish(context.Background(), official.Request{
				Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
				Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
				Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
			})
			var toolkitError *lpkgo.Error
			if !errors.As(err, &toolkitError) || toolkitError.Code != lpkgo.CodeRemoteUnavailable || toolkitError.Op != "store.official.publish" || toolkitError.StatusCode != 0 || toolkitError.Retryable || uploads != 1 {
				t.Fatalf("err=%#v uploads=%d", toolkitError, uploads)
			}
		})
	}
}

func TestPublisherRetryRepeatsStatuslessRemoteUnavailable(t *testing.T) {
	path, digest := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	httpClient := server.Client()
	baseTransport := httpClient.Transport
	checks := 0
	httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/v3/developer/app/check/exist" {
			checks++
			if checks == 1 {
				return nil, errors.New("transient network failure")
			}
		}
		return baseTransport.RoundTrip(request)
	})
	result, err := (official.Publisher{
		BaseURL: server.URL, HTTPClient: httpClient,
		NewDelay: func(_, _ time.Duration) func() time.Duration {
			return func() time.Duration { return 0 }
		},
		Wait: func(context.Context, time.Duration) error { return nil },
	}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
		Retry: config.OfficialRetry{Enabled: true, MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil || !result.Published || checks != 2 {
		t.Fatalf("err=%v result=%#v checks=%d", err, result, checks)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("truncated response")
}

type readCloserFunc func([]byte) (int, error)

func (read readCloserFunc) Read(data []byte) (int, error) {
	return read(data)
}

func (readCloserFunc) Close() error {
	return nil
}

type manualContext struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newManualContext() *manualContext {
	return &manualContext{done: make(chan struct{})}
}

func (*manualContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (ctx *manualContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *manualContext) Err() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.err
}

func (*manualContext) Value(any) any {
	return nil
}

func (ctx *manualContext) finish(err error) {
	ctx.once.Do(func() {
		ctx.mu.Lock()
		ctx.err = err
		ctx.mu.Unlock()
		close(ctx.done)
	})
}

type countingTokenProvider struct {
	token string
	calls int
}

func (provider *countingTokenProvider) Token(context.Context) (string, error) {
	provider.calls++
	return provider.token, nil
}

func TestPublisherReportsOfficialUploadFailureStage(t *testing.T) {
	path, digest := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			http.Error(response, "rejected", http.StatusBadRequest)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Op != "store.official.upload" || toolkitError.StatusCode != http.StatusBadRequest {
		t.Fatalf("err=%v", err)
	}
}

func TestPublisherReportsOfficialReviewFailureStage(t *testing.T) {
	path, digest := publishLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"message":"version already pending review"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) || toolkitError.Op != "store.official.review" || toolkitError.StatusCode != http.StatusBadRequest {
		t.Fatalf("err=%v", err)
	}
	var publicDetail interface{ PublicErrorDetail() string }
	if !errors.As(toolkitError.Cause, &publicDetail) || publicDetail.PublicErrorDetail() != "version already pending review" {
		t.Fatalf("public detail=%#v", publicDetail)
	}
}

func TestPublisherGuardRechecksWaitingReviewImmediatelyBeforeUpload(t *testing.T) {
	path, digest := publishLPK(t)
	for _, test := range []struct {
		name          string
		waiting       string
		candidate     string
		continueNewer bool
		wantPending   bool
		wantUploads   int
	}{
		{name: "equal pauses", waiting: "1.0.0", candidate: "1.0.0", continueNewer: true, wantPending: true},
		{name: "older continues", waiting: "0.9.0", candidate: "1.0.0", continueNewer: true, wantUploads: 1},
		{name: "disabled pauses older", waiting: "0.9.0", candidate: "1.0.0", wantPending: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			uploads := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/waiting":
					_, _ = fmt.Fprintf(response, `{"version":{"name":%q}}`, test.waiting)
				case "/api/v3/developer/app/check/exist":
					_, _ = response.Write([]byte(`{"exist":true}`))
				case "/api/v3/developer/app/lpk/upload":
					uploads++
					_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":%q,"iconPath":"/icon.png","url":"/demo.lpk","sha256":%q,"unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, test.candidate, digest)
				case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
					_, _ = response.Write([]byte(`{"success":true}`))
				default:
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)
			_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(t.Context(), official.Request{
				Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
				Version: test.candidate, SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
				GuardPendingReview: true, ContinueIfNewerVersion: test.continueNewer,
			})
			var pending *official.PendingReviewError
			if test.wantPending {
				if !errors.As(err, &pending) || pending.Version != test.waiting || uploads != 0 {
					t.Fatalf("err=%v pending=%#v uploads=%d", err, pending, uploads)
				}
				return
			}
			if err != nil || uploads != test.wantUploads {
				t.Fatalf("err=%v uploads=%d", err, uploads)
			}
		})
	}
}

func TestPublisherUsesToolkitProtocolAndReturnsVerifiedResult(t *testing.T) {
	path, digest := publishLPK(t)
	var created, uploaded, reviewed bool
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-User-Token") != "ci-token" {
			t.Errorf("missing X-User-Token")
		}
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":false}`))
		case "/api/v3/developer/app/create":
			created = true
			var input map[string]any
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input["package"] != "cloud.lazycat.apps.publish-demo" || input["language"] != "zh" || input["name"] != "Configured Name" {
				t.Errorf("create input=%#v", input)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		case "/api/v3/developer/app/lpk/upload":
			uploaded = true
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviewed = true
			var body struct {
				Version struct {
					Changelogs map[string]string `json:"changelogs"`
				} `json:"version"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Version.Changelogs["zh"] != "Release notes" || body.Version.Changelogs["en"] != "Release notes" {
				t.Errorf("changelogs=%#v", body.Version.Changelogs)
			}
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, FileName: "application.lpk",
		PackageID: "cloud.lazycat.apps.publish-demo", Version: "1.0.0", SHA256: digest,
		Changelog: "Release notes", Locales: []string{"zh", "en"}, CreateIfMissing: true,
		Application: config.OfficialApplication{Language: "zh", Name: "Configured Name"}, DefaultName: "Package Name",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !uploaded || !reviewed || !result.Published || !result.Created || result.PackageID != "cloud.lazycat.apps.publish-demo" || result.Version != "1.0.0" || result.SHA256 != digest || result.UploadURL != "/demo.lpk" {
		t.Fatalf("created=%v uploaded=%v reviewed=%v result=%#v", created, uploaded, reviewed, result)
	}
}

func TestPublisherUploadsLPKWithTemplateControls(t *testing.T) {
	path, digest := publishTemplatedLPK(t)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			if err := request.ParseMultipartForm(2 << 20); err != nil {
				t.Error(err)
			}
			file, _, err := request.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				data, readErr := io.ReadAll(file)
				closeErr := file.Close()
				if readErr != nil || closeErr != nil {
					t.Error(errors.Join(readErr, closeErr))
				}
				uploadedDigest := fmt.Sprintf("%x", sha256.Sum256(data))
				if uploadedDigest != digest {
					t.Errorf("uploaded digest=%s want=%s", uploadedDigest, digest)
				}
			}
			_, _ = fmt.Fprintf(response, `{"package":"cloud.lazycat.apps.publish-demo","version":"1.0.0","iconPath":"/icon.png","url":"/demo.lpk","sha256":"%s","unsupportedPlatforms":[],"minOsVersion":"1.3.0","lpkSize":123,"imageSize":0}`, digest)
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	result, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || result.PackageID != "cloud.lazycat.apps.publish-demo" || result.Version != "1.0.0" || result.SHA256 != digest {
		t.Fatalf("result=%#v", result)
	}
}

func TestPublisherRejectsUntrustedUploadMetadata(t *testing.T) {
	path, digest := publishLPK(t)
	reviewed := false
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v3/developer/app/check/exist":
			_, _ = response.Write([]byte(`{"exist":true}`))
		case "/api/v3/developer/app/lpk/upload":
			_, _ = response.Write([]byte(`{"package":"cloud.lazycat.apps.publish-demo","version":"9.9.9","url":"/demo.lpk","sha256":"bad"}`))
		case "/api/v3/developer/app/cloud.lazycat.apps.publish-demo/review/create":
			reviewed = true
			_, _ = response.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	_, err := (official.Publisher{BaseURL: server.URL, HTTPClient: server.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err == nil || reviewed {
		t.Fatalf("err=%v reviewed=%v", err, reviewed)
	}
}

func TestPublisherDoesNotForwardTokenAcrossRedirect(t *testing.T) {
	path, digest := publishLPK(t)
	reached := false
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		reached = true
		forwarded = request.Header.Get("X-User-Token") != "" || request.Header.Get("Cookie") != ""
		_, _ = response.Write([]byte(`{"exist":true}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL+request.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	_, err := (official.Publisher{BaseURL: origin.URL, HTTPClient: origin.Client()}).Publish(context.Background(), official.Request{
		Provider: auth.StaticToken("ci-token"), LPKPath: path, PackageID: "cloud.lazycat.apps.publish-demo",
		Version: "1.0.0", SHA256: digest, Changelog: "Release notes", Locales: []string{"en"},
	})
	if err == nil || reached || forwarded {
		t.Fatalf("err=%v reached=%v forwarded=%v", err, reached, forwarded)
	}
}

func publishLPK(t *testing.T) (string, string) {
	return publishLPKWithManifest(t, "application:\n  subdomain: publish-demo\n  image: registry.lazycat.cloud/demo/app:1.0.0\n", false)
}

func publishTemplatedLPK(t *testing.T) (string, string) {
	return publishLPKWithManifest(t, `application:
  subdomain: publish-demo
  image: registry.lazycat.cloud/demo/app:1.0.0
{{- if .U.multi_instance }}
  multi_instance: true
{{- end }}
`, true)
}

func publishLPKWithManifest(t *testing.T, manifestData string, allowTemplate bool) (string, string) {
	t.Helper()
	root := fstest.MapFS{
		"package.yml":  {Data: []byte("package: cloud.lazycat.apps.publish-demo\nversion: 1.0.0\nname: Publish Demo\nmin_os_version: 1.3.0\nlocales:\n  en:\n    name: Publish Demo\n"), Mode: 0o644},
		"manifest.yml": {Data: []byte(manifestData), Mode: 0o644},
		"icon.png":     {Data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, Mode: 0o644},
	}
	path := filepath.Join(t.TempDir(), "application.lpk")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lpk.Write(context.Background(), file, lpk.WriteRequest{Layout: lpk.LayoutV2, Files: root, Strict: true, AllowManifestTemplate: allowTemplate}); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return path, fmt.Sprintf("%x", digest[:])
}

func writeApplicationScreenshot(t *testing.T, path string, fill color.RGBA) {
	t.Helper()
	output := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := range 480 {
		for x := range 640 {
			output.SetRGBA(x, y, fill)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(file, output)
	closeErr := file.Close()
	if encodeErr != nil || closeErr != nil {
		t.Fatal(errors.Join(encodeErr, closeErr))
	}
}
