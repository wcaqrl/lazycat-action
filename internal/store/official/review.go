package official

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"github.com/lib-x/lzc-toolkit-go/appstore"
	"github.com/lib-x/lzc-toolkit-go/auth"
	"github.com/wcaqrl/lazycat-action/internal/httpx"
)

type Review struct {
	ID            int64     `json:"id"`
	Status        int16     `json:"status"`
	Reason        string    `json:"reason"`
	SubmitChannel uint8     `json:"submit_channel"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Version       struct {
		Name    string `json:"name"`
		Package string `json:"package"`
		SHA256  string `json:"pkg_hash"`
	} `json:"version"`
}

func (review Review) StatusName() string {
	switch review.Status {
	case -4:
		return "deleted"
	case -3:
		return "draft"
	case -2:
		return "canceled"
	case -1:
		return "rejected"
	case 0:
		return "waiting"
	case 1:
		return "approved"
	default:
		return "unknown"
	}
}

type ReviewLookup struct {
	PackageID string
	ID        int64
	Version   string
	SHA256    string
	CreatedAt time.Time
	StartedAt time.Time
}

// FindReview uses a fixed creation window, never the previous polling time.
// Legacy locks without a timestamp are scanned once, newest ID first.
func (publisher Publisher) FindReview(ctx context.Context, provider auth.TokenProvider, lookup ReviewLookup) (*Review, error) {
	if ctx == nil || provider == nil || lookup.PackageID == "" || lookup.ID == 0 && lookup.Version == "" {
		return nil, publishError(lpkgo.CodeInvalidArgument, errors.New("review lookup requires provider, package and review ID or version"))
	}
	if !publisher.SDK {
		return nil, publishError(lpkgo.CodeInvalidArgument, errors.New("review reconciliation requires an LZC_API_TOKEN developer PAT"))
	}
	baseURL := strings.TrimRight(publisher.BaseURL, "/")
	if baseURL == "" {
		baseURL = appstore.DefaultPATBaseURL
	}
	client, err := appstore.NewPATHTTPClient(baseURL, publisher.HTTPClient)
	if err != nil {
		return nil, err
	}
	client = httpx.NoRedirect(client, 30*time.Second)
	token, err := provider.Token(ctx)
	if err != nil {
		return nil, sanitizePublishError(err)
	}
	query := url.Values{"size": {"50"}, "sort": {"-id"}}
	var start, end time.Time
	if !lookup.CreatedAt.IsZero() {
		start, end = lookup.CreatedAt.Add(-5*time.Minute), lookup.CreatedAt.Add(5*time.Minute)
	} else if !lookup.StartedAt.IsZero() {
		start, end = lookup.StartedAt.Add(-5*time.Minute), time.Now().UTC().Add(5*time.Minute)
	}
	if !start.IsZero() {
		shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
		query.Set("created_at_start", start.In(shanghai).Format("2006-01-02 15:04:05"))
		query.Set("created_at_end", end.In(shanghai).Format("2006-01-02 15:04:05"))
	}
	for page := 0; ; page++ {
		query.Set("page", strconv.Itoa(page))
		target := baseURL + "/api/v3/developer/app/" + url.PathEscape(lookup.PackageID) + "/review/list?" + query.Encode()
		request, err := authenticatedRequest(ctx, http.MethodGet, target, token, nil)
		if err != nil {
			return nil, err
		}
		body, err := doRequest(client, request, "store.official.review-list")
		if err != nil {
			return nil, err
		}
		var result struct {
			Items []Review `json:"items"`
			Total *int64   `json:"total"`
		}
		if err := json.Unmarshal(body, &result); err != nil || result.Total == nil || *result.Total < 0 || len(result.Items) == 0 && *result.Total > int64(page*50) {
			return nil, publishError(lpkgo.CodeRemoteUnavailable, errors.New("invalid official review list response"))
		}
		for _, review := range result.Items {
			if lookup.ID != 0 {
				if review.ID == lookup.ID {
					return &review, nil
				}
				continue
			}
			if review.ID > 0 && review.Version.Name == lookup.Version && (review.Version.Package == "" || review.Version.Package == lookup.PackageID) &&
				(review.SubmitChannel == reviewSubmitChannelAutomation || review.SubmitChannel == 0) &&
				(lookup.SHA256 == "" || strings.EqualFold(review.Version.SHA256, lookup.SHA256)) {
				return &review, nil
			}
		}
		if len(result.Items) == 0 || int64((page+1)*50) >= *result.Total {
			return nil, nil
		}
	}
}
