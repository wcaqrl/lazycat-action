package storelookup

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cloudflare/backoff"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
	officialstore "github.com/lib-x/lzc-toolkit-go/appstore/official"
	privatestore "github.com/lib-x/lzc-toolkit-go/appstore/private"
)

type Store string

const (
	StoreOfficial Store = "official"
	StorePrivate  Store = "private"
)

type Request struct {
	Store      Store
	PackageID  string
	BaseURL    string
	GroupCodes []string
	HTTPClient *http.Client
	Retry      RetryPolicy
}

type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type Result struct {
	OnlineVersion string
}

type Lookup func(context.Context, Request) (Result, error)

func Default(ctx context.Context, request Request) (Result, error) {
	policy := normalizedRetryPolicy(request.Retry)
	retryBackoff := backoff.New(policy.MaxDelay, policy.InitialDelay)
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		result, err := lookupOnce(ctx, request)
		if err == nil {
			return result, nil
		}
		if attempt == policy.MaxAttempts || !retryableLookupError(err) {
			return Result{}, err
		}
		if err := waitForRetry(ctx, retryBackoff.Duration()); err != nil {
			return Result{}, err
		}
	}
	panic("store lookup retry loop exhausted without returning")
}

func lookupOnce(ctx context.Context, request Request) (Result, error) {
	var version string
	switch request.Store {
	case StoreOfficial:
		client := officialstore.New(officialstore.Options{
			MetadataBaseURL: strings.TrimSpace(request.BaseURL),
			HTTPClient:      request.HTTPClient,
		})
		application, err := client.Application(ctx, request.PackageID)
		if err != nil {
			return Result{}, err
		}
		version = application.Version.Name
	case StorePrivate:
		client, err := privatestore.New(privatestore.Options{
			BaseURL:    strings.TrimSpace(request.BaseURL),
			HTTPClient: request.HTTPClient,
			GroupCodes: append([]string(nil), request.GroupCodes...),
		})
		if err != nil {
			return Result{}, err
		}
		latest, err := client.LatestVersion(ctx, privatestore.LatestVersionRequest{PackageID: request.PackageID})
		if err != nil {
			return Result{}, err
		}
		version = latest.LatestVersion.Version
	default:
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: "storelookup", Cause: errors.New("unsupported store")}
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return Result{}, &lpkgo.Error{Code: lpkgo.CodeRemoteUnavailable, Op: "storelookup", Cause: errors.New("store returned an empty latest version")}
	}
	return Result{OnlineVersion: version}, nil
}

func normalizedRetryPolicy(policy RetryPolicy) RetryPolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 3
	}
	if policy.InitialDelay <= 0 {
		policy.InitialDelay = time.Second
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 4 * time.Second
	}
	if policy.MaxDelay < policy.InitialDelay {
		policy.MaxDelay = policy.InitialDelay
	}
	return policy
}

func retryableLookupError(err error) bool {
	var toolkitError *lpkgo.Error
	if !errors.As(err, &toolkitError) {
		return false
	}
	switch toolkitError.Code {
	case lpkgo.CodeInvalidArgument,
		lpkgo.CodeInvalidConfig,
		lpkgo.CodeInvalidManifest,
		lpkgo.CodeUnauthenticated,
		lpkgo.CodePermissionDenied,
		lpkgo.CodeNotFound,
		lpkgo.CodeConflict,
		lpkgo.CodeCommandFailed,
		lpkgo.CodeIntegrityMismatch,
		lpkgo.CodeCancelled,
		lpkgo.CodeDeadlineExceeded:
		return false
	}
	if toolkitError.Code != lpkgo.CodeRemoteUnavailable {
		return false
	}
	if toolkitError.StatusCode == 0 {
		return toolkitError.Retryable
	}
	if toolkitError.StatusCode >= http.StatusOK && toolkitError.StatusCode < http.StatusMultipleChoices {
		return true
	}
	return toolkitError.StatusCode == http.StatusTooManyRequests ||
		toolkitError.StatusCode >= http.StatusInternalServerError && toolkitError.StatusCode < 600
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
