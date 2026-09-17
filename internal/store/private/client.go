package private

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/httpx"
	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

const maxResponseBytes = 1 << 20

var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Options struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func New(options Options) (*Client, error) {
	baseURL, err := validateBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(options.Token)
	if token == "" {
		return nil, clientError(lpkgo.CodeUnauthenticated, 0, false, errors.New("private store token is required"))
	}
	httpClient := httpx.NoRedirect(options.HTTPClient, 30*time.Second)
	return &Client{baseURL: baseURL, token: token, httpClient: httpClient}, nil
}

func (client *Client) Publish(ctx context.Context, request Request) (Result, error) {
	request = normalizeRequest(request)
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}
	var application appDTO
	var found bool
	var resolvedByName bool
	var err error
	if request.AppID != "" {
		if _, parseErr := strconv.ParseUint(request.AppID, 10, 64); parseErr != nil || request.AppID == "0" {
			return Result{}, clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("APP_ID must be a positive integer"))
		}
		application, err = client.getApplication(ctx, request.AppID)
		found = err == nil
	} else {
		application, found, resolvedByName, err = client.findApplication(ctx, request.PackageID, request.Name)
		if err == nil && found {
			application, err = client.getApplication(ctx, string(application.ID))
		}
	}
	if err != nil {
		return Result{}, err
	}
	if !found {
		return client.createApplication(ctx, request)
	}
	if resolvedByName && strings.TrimSpace(application.Name) != request.Name {
		return Result{}, clientError(lpkgo.CodeRemoteUnavailable, http.StatusOK, false, errors.New("private store resolved a different application name"))
	}
	if !resolvedByName && strings.TrimSpace(application.PackageID) != request.PackageID {
		return Result{}, clientError(lpkgo.CodeConflict, 0, false, errors.New("private store application packageId does not match the LPK"))
	}
	if existing, ok := matchingVersion(application, request.Version); ok {
		if err := verifyVersionIdentity(existing, request.Version, application.ID); err != nil {
			return Result{}, err
		}
		if strings.EqualFold(strings.TrimSpace(existing.SHA256), request.SHA256) && strings.TrimSpace(existing.DownloadURL) == request.DownloadURL {
			return resultFromVersion(application, existing, false, true), nil
		}
		return Result{}, clientError(lpkgo.CodeConflict, 0, false, errors.New("private store version already exists with different content"))
	}
	return client.createVersion(ctx, application, request)
}

func (client *Client) findApplication(ctx context.Context, packageID, name string) (appDTO, bool, bool, error) {
	application, found, err := client.findApplicationByQuery(ctx, packageID, packageID)
	if err != nil || found || strings.TrimSpace(name) == strings.TrimSpace(packageID) {
		return application, found, false, err
	}
	application, found, err = client.findApplicationByName(ctx, name)
	return application, found, found, err
}

func (client *Client) findApplicationByName(ctx context.Context, name string) (appDTO, bool, error) {
	var response struct {
		App appDTO `json:"app"`
	}
	endpoint := "/api/v1/apps/by-name?name=" + url.QueryEscape(name)
	if err := client.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		if errors.Is(err, lpkgo.ErrNotFound) {
			return appDTO{}, false, nil
		}
		return appDTO{}, false, err
	}
	if !validIdentifier(response.App.ID) || strings.TrimSpace(response.App.Name) != strings.TrimSpace(name) || !response.App.CanUpload {
		return appDTO{}, false, clientError(lpkgo.CodeRemoteUnavailable, http.StatusOK, false, errors.New("private store returned invalid name resolution metadata"))
	}
	return response.App, true, nil
}

func (client *Client) findApplicationByQuery(ctx context.Context, query, packageID string) (appDTO, bool, error) {
	var response struct {
		Apps []appDTO `json:"apps"`
	}
	endpoint := "/api/v1/apps?q=" + url.QueryEscape(query)
	if err := client.doJSON(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return appDTO{}, false, err
	}
	var match appDTO
	found := false
	for _, application := range response.Apps {
		if strings.TrimSpace(application.PackageID) != packageID {
			continue
		}
		if found {
			return appDTO{}, false, clientError(lpkgo.CodeConflict, 0, false, errors.New("private store returned duplicate packageId entries"))
		}
		match = application
		found = true
	}
	return match, found, nil
}

func (client *Client) getApplication(ctx context.Context, appID string) (appDTO, error) {
	var response struct {
		App appDTO `json:"app"`
	}
	if err := client.doJSON(ctx, http.MethodGet, "/api/v1/apps/"+url.PathEscape(appID), nil, &response); err != nil {
		return appDTO{}, err
	}
	if !validIdentifier(response.App.ID) || strings.TrimSpace(response.App.PackageID) == "" {
		return appDTO{}, clientError(lpkgo.CodeRemoteUnavailable, http.StatusOK, false, errors.New("private store returned incomplete application metadata"))
	}
	return response.App, nil
}

func (client *Client) createApplication(ctx context.Context, request Request) (Result, error) {
	input := createApplicationRequest{
		PackageID: request.PackageID, Name: request.Name, Summary: request.Summary, Version: request.Version,
		Changelog: request.Changelog, SourceType: "GITHUB", DownloadURL: request.DownloadURL, SHA256: request.SHA256,
	}
	var response struct {
		App appDTO `json:"app"`
	}
	if err := client.doJSON(ctx, http.MethodPost, "/api/v1/apps", input, &response); err != nil {
		return Result{}, err
	}
	if !validIdentifier(response.App.ID) || strings.TrimSpace(response.App.PackageID) != request.PackageID || response.App.LatestVersion == nil {
		return Result{}, clientError(lpkgo.CodeRemoteUnavailable, http.StatusCreated, false, errors.New("private store returned incomplete created application metadata"))
	}
	version := *response.App.LatestVersion
	if err := verifyVersion(version, request, response.App.ID); err != nil {
		return Result{}, err
	}
	return resultFromVersion(response.App, version, true, false), nil
}

func (client *Client) createVersion(ctx context.Context, application appDTO, request Request) (Result, error) {
	input := createVersionRequest{
		Version: request.Version, Changelog: request.Changelog, SourceType: "GITHUB",
		DownloadURL: request.DownloadURL, SHA256: request.SHA256,
	}
	var response struct {
		Version versionDTO `json:"version"`
	}
	endpoint := "/api/v1/apps/" + url.PathEscape(string(application.ID)) + "/versions"
	if err := client.doJSON(ctx, http.MethodPost, endpoint, input, &response); err != nil {
		return Result{}, err
	}
	if err := verifyVersion(response.Version, request, application.ID); err != nil {
		return Result{}, err
	}
	return resultFromVersion(application, response.Version, false, false), nil
}

func (client *Client) doJSON(ctx context.Context, method, endpoint string, input, output any) error {
	if ctx == nil {
		return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("context is required"))
	}
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("unable to encode private store request"))
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+endpoint, body)
	if err != nil {
		return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("unable to create private store request"))
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+client.token)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return clientError(lpkgo.CodeCancelled, 0, false, ctx.Err())
		}
		return clientError(lpkgo.CodeRemoteUnavailable, 0, true, errors.New("private store request failed"))
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil || len(data) > maxResponseBytes {
		return clientError(lpkgo.CodeRemoteUnavailable, response.StatusCode, response.StatusCode >= 500, errors.New("private store response is invalid or too large"))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return statusError(response.StatusCode)
	}
	if output != nil && json.Unmarshal(data, output) != nil {
		return clientError(lpkgo.CodeRemoteUnavailable, response.StatusCode, false, errors.New("private store returned invalid JSON"))
	}
	return nil
}

func normalizeRequest(request Request) Request {
	request.AppID = strings.TrimSpace(request.AppID)
	request.PackageID = strings.TrimSpace(request.PackageID)
	request.Name = strings.TrimSpace(request.Name)
	request.Summary = strings.TrimSpace(request.Summary)
	request.Version = strings.TrimSpace(request.Version)
	request.Changelog = strings.TrimSpace(request.Changelog)
	request.DownloadURL = strings.TrimSpace(request.DownloadURL)
	request.SHA256 = strings.ToLower(strings.TrimSpace(request.SHA256))
	return request
}

func validateRequest(request Request) error {
	if request.PackageID == "" || request.Name == "" || request.Version == "" || request.DownloadURL == "" || !sha256Pattern.MatchString(request.SHA256) {
		return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("packageId, name, version, downloadUrl, and SHA256 are required"))
	}
	parsed, err := url.Parse(request.DownloadURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("downloadUrl must be an HTTPS GitHub Release Asset URL"))
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 6 || segments[2] != "releases" || segments[3] != "download" || segments[0] == "" || segments[1] == "" || segments[4] == "" || segments[5] == "" {
		return clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("downloadUrl must identify a GitHub Release Asset"))
	}
	return nil
}

func validateBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("APPSTORE_URL must be an absolute store root URL"))
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return "", clientError(lpkgo.CodeInvalidArgument, 0, false, errors.New("APPSTORE_URL must use HTTPS"))
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func matchingVersion(application appDTO, version string) (versionDTO, bool) {
	for _, candidate := range application.Versions {
		if strings.TrimSpace(candidate.Version) == version {
			return candidate, true
		}
	}
	if application.LatestVersion != nil && strings.TrimSpace(application.LatestVersion.Version) == version {
		return *application.LatestVersion, true
	}
	return versionDTO{}, false
}

func verifyVersion(version versionDTO, request Request, appID identifier) error {
	if err := verifyVersionIdentity(version, request.Version, appID); err != nil {
		return err
	}
	if strings.TrimSpace(version.DownloadURL) != request.DownloadURL || !strings.EqualFold(strings.TrimSpace(version.SHA256), request.SHA256) {
		return clientError(lpkgo.CodeRemoteUnavailable, http.StatusCreated, false, errors.New("private store version response does not match the verified LPK"))
	}
	return nil
}

func verifyVersionIdentity(version versionDTO, expectedVersion string, appID identifier) error {
	if strings.TrimSpace(version.Version) != expectedVersion || !validIdentifier(version.ID) || !validIdentifier(version.AppID) || version.AppID != appID {
		return clientError(lpkgo.CodeRemoteUnavailable, http.StatusOK, false, errors.New("private store returned invalid version identity"))
	}
	return nil
}

func validIdentifier(id identifier) bool {
	value, err := strconv.ParseUint(strings.TrimSpace(string(id)), 10, 64)
	return err == nil && value > 0
}

func resultFromVersion(application appDTO, version versionDTO, created, existing bool) Result {
	return Result{
		Published: true, Created: created, Existing: existing, AppID: string(application.ID), VersionID: string(version.ID),
		PackageID: strings.TrimSpace(application.PackageID), Version: strings.TrimSpace(version.Version),
		DownloadURL: strings.TrimSpace(version.DownloadURL), SHA256: strings.ToLower(strings.TrimSpace(version.SHA256)),
	}
}

func statusError(status int) error {
	code := lpkgo.CodeRemoteUnavailable
	switch status {
	case http.StatusUnauthorized:
		code = lpkgo.CodeUnauthenticated
	case http.StatusForbidden:
		code = lpkgo.CodePermissionDenied
	case http.StatusNotFound:
		code = lpkgo.CodeNotFound
	case http.StatusConflict, http.StatusUnprocessableEntity:
		code = lpkgo.CodeConflict
	}
	return clientError(code, status, status == http.StatusTooManyRequests || status >= 500, errors.New("private store rejected the request"))
}

func clientError(code lpkgo.Code, status int, retryable bool, cause error) error {
	return &lpkgo.Error{Code: code, Op: "store.private", StatusCode: status, Retryable: retryable, Cause: fmt.Errorf("%w", cause)}
}
