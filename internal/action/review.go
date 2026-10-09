package action

import (
	"context"
	"time"

	"github.com/wcaqrl/lazycat-action/internal/pipelinestate"
	"github.com/wcaqrl/lazycat-action/internal/source"
	"github.com/wcaqrl/lazycat-action/internal/store/official"
)

func reconcileReviews(ctx context.Context, lock *pipelinestate.Lock, packageID string, deps Dependencies) (bool, error) {
	if lock.Fingerprint == "" {
		return false, nil
	}
	changed := false
	if lock.Status == "submitted" {
		found := false
		for _, review := range lock.Reviews {
			if review.Fingerprint == lock.Fingerprint && review.ApplicationVersion == lock.Application.Version {
				found = true
			}
		}
		if !found {
			lock.RecordReview(pipelinestate.Review{Source: lock.Source, Fingerprint: lock.Fingerprint, ApplicationVersion: lock.Application.Version, LPKSHA256: lock.Application.LPKSHA256, Status: "submitted"})
			changed = true
		}
	}
	if deps.LookupReview == nil {
		return changed, nil
	}
	for i := range lock.Reviews {
		record := &lock.Reviews[i]
		if record.Status != "waiting" && record.Status != "submitted" && record.Status != "draft" && record.Status != "unknown" {
			continue
		}
		started := record.SubmittedAt
		if started.IsZero() && record.ApplicationVersion == lock.Application.Version {
			started = lock.PackagedAt
		}
		review, err := deps.LookupReview(ctx, official.ReviewLookup{PackageID: packageID, ID: record.ID, Version: record.ApplicationVersion, SHA256: record.LPKSHA256, CreatedAt: record.CreatedAt, StartedAt: started})
		if err != nil {
			return false, err
		}
		if review != nil && applyReview(record, *review) {
			changed = true
		}
		if review == nil && record.ID == 0 && record.CreatedAt.IsZero() && started.IsZero() {
			// Do not scan all legacy history every day if no matching record exists.
			record.Status, record.Reason, record.CheckedAt = "untracked", "No matching historical review was found during lock migration", time.Now().UTC()
			changed = true
		}
	}
	// A packaged lock was pushed before submission. Recover a lost submitted commit
	// from the server using that persisted timestamp and the exact LPK hash.
	if lock.Status == "packaged" {
		review, err := deps.LookupReview(ctx, official.ReviewLookup{PackageID: packageID, Version: lock.Application.Version, SHA256: lock.Application.LPKSHA256, StartedAt: lock.PackagedAt})
		if err != nil {
			return false, err
		}
		if review != nil {
			record := pipelinestate.Review{Source: lock.Source, Fingerprint: lock.Fingerprint, ApplicationVersion: lock.Application.Version, LPKSHA256: lock.Application.LPKSHA256}
			applyReview(&record, *review)
			lock.RecordReview(record)
			lock.Status = "submitted"
			changed = true
		}
	}
	return changed, nil
}

func applyReview(record *pipelinestate.Review, review official.Review) bool {
	status := review.StatusName()
	if record.ID == review.ID && record.Status == status && record.Reason == review.Reason && record.CreatedAt.Equal(review.CreatedAt) {
		return false
	}
	record.ID, record.Status, record.Reason, record.CreatedAt = review.ID, status, review.Reason, review.CreatedAt
	if record.SubmittedAt.IsZero() {
		record.SubmittedAt = review.CreatedAt
	}
	record.CheckedAt = time.Now().UTC()
	return true
}

func processedSource(lock pipelinestate.Lock, candidate source.Candidate) (processed, rejected bool) {
	for _, review := range lock.Reviews {
		if pipelinestate.SameSource(review.Source, candidate) {
			processed, rejected = true, review.Status == "rejected"
		}
	}
	if !processed && lock.Status == "submitted" && pipelinestate.SameSource(lock.Source, candidate) {
		return true, false
	}
	return processed, rejected
}

func resumableSource(lock pipelinestate.Lock, candidate source.Candidate) bool {
	if lock.Status != "packaged" || !pipelinestate.SameSource(lock.Source, candidate) {
		return false
	}
	for _, review := range lock.Reviews {
		if review.ApplicationVersion == lock.Application.Version {
			return false
		}
	}
	return true
}

func currentReviewStatus(lock pipelinestate.Lock) string {
	for i := len(lock.Reviews) - 1; i >= 0; i-- {
		if lock.Reviews[i].ApplicationVersion == lock.Application.Version && lock.Reviews[i].Fingerprint == lock.Fingerprint {
			return lock.Reviews[i].Status
		}
	}
	return ""
}
