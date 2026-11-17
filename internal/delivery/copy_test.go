package delivery_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/wcaqrl/lazycat-action/internal/config"
	"github.com/wcaqrl/lazycat-action/internal/delivery"
)

const copiedImage = `{"finished":true,"lzc_image":"registry.lazycat.cloud/acme/web:copied"}`

func copyServer(t *testing.T, progress func(http.ResponseWriter, *http.Request, int)) (delivery.Resolver, *bytes.Buffer, *atomic.Int64) {
	t.Helper()
	var starts, polls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("image") != "ghcr.io/acme/web:v1" || r.URL.Query().Get("platform") != "amd64" {
			t.Error("incorrect copy identity")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/copy") {
			starts.Add(1)
			fmt.Fprint(w, `{}`)
			return
		}
		progress(w, r, int(polls.Add(1)))
	}))
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	client := appstore.New(appstore.Options{BaseURL: server.URL, HTTPClient: server.Client(), Token: auth.StaticToken("test-token"), PollInterval: 10 * time.Millisecond})
	return delivery.Resolver{Copier: client, Logger: slog.New(slog.NewTextHandler(output, nil))}, output, &starts
}

func copyRequest(total, idle time.Duration) delivery.Request {
	return delivery.Request{Image: config.Image{ID: "web", Delivery: config.Delivery{Mode: "lazycat", CopyTimeout: total, CopyIdleTimeout: idle}}, SourceRef: "ghcr.io/acme/web:v1"}
}

func TestCopyLogsFinalizationAndWaitsForOfficialCompletion(t *testing.T) {
	resolver, output, starts := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 4 {
			fmt.Fprint(w, `{"finished":false,"layers":[{"hash":"sha256:abc","progress":100}]}`)
			return
		}
		fmt.Fprint(w, copiedImage)
	})
	callbacks := 0
	request := copyRequest(time.Second, 200*time.Millisecond)
	request.OnProgress = func(appstore.CopyProgress) { callbacks++ }
	got, err := resolver.Deliver(t.Context(), request)
	if err != nil || got.RuntimeRef != "registry.lazycat.cloud/acme/web:copied" || callbacks != 4 || starts.Load() != 1 {
		t.Fatalf("result=%+v callbacks=%d starts=%d err=%v", got, callbacks, starts.Load(), err)
	}
	for _, want := range []string{"image_id=web", "elapsed=", "copy started", "waiting-for-finalization", "completed_layers=1", "copy completed"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("missing %q in %s", want, output.String())
		}
	}
}

func TestUnchangedAndReorderedProgressCannotEvadeIdleTimeout(t *testing.T) {
	resolver, output, starts := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n%2 == 0 {
			fmt.Fprint(w, `{"finished":false,"layers":[{"hash":"b","progress":100},{"hash":"a","progress":50}]}`)
		} else {
			fmt.Fprint(w, `{"finished":false,"layers":[{"hash":"a","progress":50},{"hash":"b","progress":100}]}`)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := resolver.Deliver(ctx, copyRequest(800*time.Millisecond, 80*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "no-progress timeout") || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "1/2") {
		t.Fatalf("err=%v logs=%s", err, output.String())
	}
	if starts.Load() != 1 {
		t.Fatal("copy was resubmitted")
	}
}

func TestActiveTransferStillObeysTotalTimeout(t *testing.T) {
	resolver, _, starts := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		fmt.Fprintf(w, `{"finished":false,"layers":[{"hash":"a","progress":%d}]}`, min(n, 99))
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := resolver.Deliver(ctx, copyRequest(130*time.Millisecond, 90*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "total timeout") {
		t.Fatalf("err=%v", err)
	}
	if starts.Load() != 1 {
		t.Fatal("copy was resubmitted")
	}
}

func TestIncreasingProgressResetsIdleAndEmptyLimitsRemainCompatible(t *testing.T) {
	resolver, _, _ := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 12 {
			fmt.Fprintf(w, `{"finished":false,"layers":[{"hash":"a","progress":%d}]}`, n)
			return
		}
		fmt.Fprint(w, copiedImage)
	})
	if _, err := resolver.Deliver(t.Context(), copyRequest(time.Second, 80*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	resolver, _, _ = copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) { fmt.Fprint(w, copiedImage) })
	if _, err := resolver.Deliver(t.Context(), copyRequest(0, 0)); err != nil {
		t.Fatal(err)
	}
}

func TestIdleTimeoutCancelsBlockedHTTPAndParentCancellationIsPreserved(t *testing.T) {
	for _, parentCancel := range []bool{false, true} {
		t.Run(fmt.Sprint(parentCancel), func(t *testing.T) {
			canceled := make(chan struct{})
			resolver, _, _ := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) { <-r.Context().Done(); close(canceled) })
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if parentCancel {
				time.AfterFunc(30*time.Millisecond, cancel)
			}
			_, err := resolver.Deliver(ctx, copyRequest(800*time.Millisecond, 80*time.Millisecond))
			if err == nil {
				t.Fatal("blocked copy succeeded")
			}
			if parentCancel && strings.Contains(err.Error(), "no-progress timeout") {
				t.Fatalf("parent cancellation replaced: %v", err)
			}
			if !parentCancel && !strings.Contains(err.Error(), "no-progress timeout") {
				t.Fatalf("err=%v", err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("copy HTTP request survived cancellation")
			}
		})
	}
}

func TestFinishedLayersWithoutFinishedFlagRemainBounded(t *testing.T) {
	resolver, _, _ := copyServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		fmt.Fprint(w, `{"finished":false,"layers":[{"hash":"a","progress":100}]}`)
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := resolver.Deliver(ctx, copyRequest(800*time.Millisecond, 80*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "no-progress timeout") {
		t.Fatalf("err=%v", err)
	}
}
