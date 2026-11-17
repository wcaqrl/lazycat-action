package delivery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/lib-x/lzc-toolkit-go/appstore"
)

var errCopyTotalTimeout = errors.New("total timeout")
var errCopyIdleTimeout = errors.New("no-progress timeout")

// CopyWaitError is safe to surface through the Action diagnostic wrapper.
type CopyWaitError struct {
	ImageID          string
	Kind             error
	Limit, Elapsed   time.Duration
	Completed, Total int
}

func (e *CopyWaitError) Error() string {
	return fmt.Sprintf("image %q copy %s after %s (elapsed %s; layers %d/%d complete)", e.ImageID, e.Kind, e.Limit, e.Elapsed.Round(time.Millisecond), e.Completed, e.Total)
}
func (e *CopyWaitError) PublicErrorDetail() string { return e.Error() }
func (e *CopyWaitError) Unwrap() error             { return e.Kind }

type copyProgress struct {
	mu                   sync.Mutex
	started, lastAdvance time.Time
	layers               map[string]int
	phase                string
}

func (p *copyProgress) observe(progress appstore.CopyProgress) (advanced, phaseChanged bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	oldPhase := p.phase
	for _, layer := range progress.Layers {
		if layer.Hash == "" || layer.Progress < 0 || layer.Progress > 100 {
			continue
		}
		last, exists := p.layers[layer.Hash]
		if !exists || layer.Progress > last {
			p.layers[layer.Hash] = layer.Progress
			advanced = true
		}
	}
	done := 0
	for _, percent := range p.layers {
		if percent == 100 {
			done++
		}
	}
	switch {
	case progress.Finished:
		p.phase = "server-finished"
	case len(p.layers) > 0 && done == len(p.layers):
		p.phase = "waiting-for-finalization"
	case len(p.layers) > 0:
		p.phase = "transferring"
	}
	if advanced {
		p.lastAdvance = time.Now()
	}
	return advanced, p.phase != oldPhase
}

func (p *copyProgress) snapshot() (elapsed, idle time.Duration, completed, total int, phase string, layers []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for hash, percent := range p.layers {
		if percent == 100 {
			completed++
		}
		short := strings.TrimPrefix(hash, "sha256:")
		if len(short) > 12 {
			short = short[:12]
		}
		layers = append(layers, fmt.Sprintf("%s=%d%%", short, percent))
	}
	sort.Strings(layers)
	return time.Since(p.started), time.Since(p.lastAdvance), completed, len(p.layers), p.phase, layers
}

func (r Resolver) copyImage(ctx context.Context, request Request, source, platform string) (appstore.CopyImageResult, error) {
	total, idle := request.Image.Delivery.CopyTimeouts()
	if total <= 0 || idle <= 0 || idle > total {
		return appstore.CopyImageResult{}, errors.New("invalid image copy timeout limits")
	}
	copyCtx, stopTotal := context.WithTimeoutCause(ctx, total, errCopyTotalTimeout)
	defer stopTotal()
	copyCtx, cancel := context.WithCancelCause(copyCtx)
	defer cancel(nil)
	now := time.Now()
	progress := &copyProgress{started: now, lastAdvance: now, layers: map[string]int{}, phase: "waiting-for-progress"}
	logProgress := func(message string, extra ...any) {
		if r.Logger == nil {
			return
		}
		elapsed, stalled, completed, count, phase, layers := progress.snapshot()
		args := []any{"image_id", request.Image.ID, "elapsed", elapsed.Round(time.Second), "idle", stalled.Round(time.Second), "completed_layers", completed, "total_layers", count, "phase", phase, "layer_progress", layers}
		r.Logger.Info(message, append(args, extra...)...)
	}
	logProgress("Docker image copy started", "source", source, "platform", platform, "copy_timeout", total, "copy_idle_timeout", idle)
	changed := make(chan struct{}, 1)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		timer := time.NewTimer(idle)
		defer timer.Stop()
		reset := func() bool {
			elapsed, stalled, _, _, _, _ := progress.snapshot()
			if stalled >= idle {
				// When both deadlines expire together (the omitted idle limit
				// can equal total), report the total cap deterministically.
				if elapsed >= total {
					cancel(errCopyTotalTimeout)
				} else {
					cancel(errCopyIdleTimeout)
				}
				return false
			}
			timer.Reset(idle - stalled)
			return true
		}
		for {
			select {
			case <-stop:
				return
			case <-copyCtx.Done():
				return
			case <-changed:
				if !reset() {
					return
				}
			case <-timer.C:
				if !reset() {
					return
				}
			case <-heartbeat.C:
				logProgress("Docker image copy progress")
			}
		}
	}()
	defer func() { close(stop); <-done }()
	var callbackMu sync.Mutex
	result, err := r.Copier.CopyImage(copyCtx, appstore.CopyImageRequest{
		Image: source, Platform: platform, Timeout: total,
		OnProgress: func(update appstore.CopyProgress) {
			callbackMu.Lock()
			defer callbackMu.Unlock()
			advanced, phaseChanged := progress.observe(update)
			if advanced {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			if phaseChanged {
				logProgress("Docker image copy progress")
			}
			if request.OnProgress != nil {
				request.OnProgress(update)
			}
		},
	})
	cause := context.Cause(copyCtx)
	if err != nil || cause != nil {
		if errors.Is(cause, errCopyTotalTimeout) || errors.Is(cause, errCopyIdleTimeout) {
			elapsed, _, completed, count, _, _ := progress.snapshot()
			limit := total
			if errors.Is(cause, errCopyIdleTimeout) {
				limit = idle
			}
			err = &CopyWaitError{ImageID: request.Image.ID, Kind: cause, Limit: limit, Elapsed: elapsed, Completed: completed, Total: count}
		} else if err == nil {
			err = copyCtx.Err()
		}
		logProgress("Docker image copy failed")
		return appstore.CopyImageResult{}, fmt.Errorf("copy image to LazyCat registry: %w", err)
	}
	ref, parseErr := name.ParseReference(strings.TrimSpace(result.LazyCatImage), name.WeakValidation)
	if strings.TrimSpace(result.SourceImage) != source || result.Platform != platform || parseErr != nil || ref.Context().RegistryStr() != "registry.lazycat.cloud" || !result.Progress.Finished {
		logProgress("Docker image copy failed")
		return appstore.CopyImageResult{}, fmt.Errorf("invalid LazyCat copy result for %q", source)
	}
	result.LazyCatImage = ref.Name()
	progress.observe(result.Progress)
	logProgress("Docker image copy completed", "image", result.LazyCatImage)
	return result, nil
}
