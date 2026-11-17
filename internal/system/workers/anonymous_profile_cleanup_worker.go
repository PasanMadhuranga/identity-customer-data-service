/*
 * Copyright (c) 2026, WSO2 LLC. (http://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package workers

import (
	"context"
	"fmt"
	"sync"
	"time"

	adminConfigStore "github.com/wso2/identity-customer-data-service/internal/admin_config/store"
	orgStore "github.com/wso2/identity-customer-data-service/internal/organization/store"
	profileStore "github.com/wso2/identity-customer-data-service/internal/profile/store"
	"github.com/wso2/identity-customer-data-service/internal/system/constants"
	"github.com/wso2/identity-customer-data-service/internal/system/log"
	"github.com/wso2/identity-customer-data-service/internal/system/utils"
)

// maxMeaningfulRetentionDays is 100 years. A longer retention deletes nothing, so the organization is skipped.
const maxMeaningfulRetentionDays = 36500

// maxAnonymousCleanupBatchesPerOrg caps one organization's share of a run; a larger backlog continues
// in the next run.
const maxAnonymousCleanupBatchesPerOrg = 200

var (
	anonymousCleanupMu sync.Mutex
	// anonymousCleanupCancel stops the worker and cancels in-flight database work.
	anonymousCleanupCancel context.CancelFunc
	// anonymousCleanupDone is closed when the worker goroutine returns.
	anonymousCleanupDone chan struct{}
)

// anonymousProfileSource is what the sweep needs from the database.
type anonymousProfileSource interface {
	// OrgIDs lists the organizations that have profiles.
	OrgIDs(ctx context.Context) ([]string, error)
	// Retentions lists the retention in days that organizations have set for themselves.
	Retentions(ctx context.Context) (map[string]int, error)
	// Primaries maps each stored organization to its primary organization.
	Primaries(ctx context.Context) (map[string]string, error)
	// DeleteExpired deletes up to batchSize anonymous profiles not updated since cutoff.
	DeleteExpired(ctx context.Context, orgID string, cutoff time.Time, batchSize int) (int, error)
}

type databaseAnonymousProfileSource struct{}

func (databaseAnonymousProfileSource) OrgIDs(ctx context.Context) ([]string, error) {
	return profileStore.ListProfileOrgIDs(ctx)
}

func (databaseAnonymousProfileSource) Retentions(ctx context.Context) (map[string]int, error) {
	return adminConfigStore.ListAnonymousProfileRetentions(ctx)
}

func (databaseAnonymousProfileSource) Primaries(ctx context.Context) (map[string]string, error) {
	return orgStore.ListPrimaryOrgIDs(ctx)
}

func (databaseAnonymousProfileSource) DeleteExpired(ctx context.Context, orgID string, cutoff time.Time,
	batchSize int) (int, error) {
	return profileStore.DeleteExpiredAnonymousProfiles(ctx, orgID, cutoff, batchSize)
}

// StartAnonymousProfileCleanupWorker deletes expired anonymous profiles every 24 hours, using each
// organization's retention.
func StartAnonymousProfileCleanupWorker() {
	logger := log.GetLogger()

	batchSize := constants.DefaultAnonymousProfileCleanupBatchSize
	defaultDays := constants.DefaultAnonymousProfileRetentionDays
	interval := time.Duration(constants.DefaultAnonymousProfileCleanupInterval) * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	anonymousCleanupMu.Lock()
	anonymousCleanupCancel = cancel
	anonymousCleanupDone = done
	anonymousCleanupMu.Unlock()

	logger.Info(fmt.Sprintf(
		"Anonymous profile cleanup worker started. Interval: %s, Batch size: %d, Default retention: %d days",
		interval, batchSize, defaultDays))

	ticker := time.NewTicker(interval)

	go func() {
		defer close(done)
		defer utils.RecoverPanic("anonymous profile cleanup worker")
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runAnonymousProfileCleanup(ctx, databaseAnonymousProfileSource{}, defaultDays, batchSize,
					time.Now())
			case <-ctx.Done():
				logger.Info("Anonymous profile cleanup worker stopped")
				return
			}
		}
	}()
}

// StopAnonymousProfileCleanupWorker cancels the sweep and waits for the goroutine to exit. A cut off
// batch is rolled back.
func StopAnonymousProfileCleanupWorker(ctx context.Context) error {
	anonymousCleanupMu.Lock()
	cancel, done := anonymousCleanupCancel, anonymousCleanupDone
	anonymousCleanupCancel, anonymousCleanupDone = nil, nil
	anonymousCleanupMu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	if done == nil {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ErrShutdownIncomplete
	}
}

// retentionDaysFor returns an organization's retention. A sub organization follows its primary; with
// none set the default applies.
func retentionDaysFor(orgID string, retentions map[string]int, primaries map[string]string,
	defaultDays int) int {
	owner := orgID
	if primary, ok := primaries[orgID]; ok && primary != "" {
		owner = primary
	}
	if days, ok := retentions[owner]; ok {
		return days
	}
	return defaultDays
}

// runAnonymousProfileCleanup deletes expired anonymous profiles of every organization and returns the
// number deleted.
func runAnonymousProfileCleanup(ctx context.Context, source anonymousProfileSource, defaultDays, batchSize int,
	now time.Time) int {
	logger := log.GetLogger()

	orgIDs, err := source.OrgIDs(ctx)
	if err != nil {
		logger.Error("Anonymous profile cleanup could not list the organizations", log.Error(err))
		return 0
	}
	retentions, err := source.Retentions(ctx)
	if err != nil {
		// Without the organizations' settings the default could override a shorter retention, so skip.
		logger.Error("Anonymous profile cleanup could not read the retention settings", log.Error(err))
		return 0
	}
	primaries, err := source.Primaries(ctx)
	if err != nil {
		logger.Error("Anonymous profile cleanup could not read the organization tree", log.Error(err))
		return 0
	}

	total := 0
	for _, orgID := range orgIDs {
		if ctx.Err() != nil {
			break
		}
		days := retentionDaysFor(orgID, retentions, primaries, defaultDays)
		if days > maxMeaningfulRetentionDays {
			// No profile is that old, and the cutoff would fall outside what the databases store.
			continue
		}
		cutoff := now.UTC().AddDate(0, 0, -days)

		deletedForOrg := 0
		for batch := 0; batch < maxAnonymousCleanupBatchesPerOrg && ctx.Err() == nil; batch++ {
			deleted, err := source.DeleteExpired(ctx, orgID, cutoff, batchSize)
			if err != nil {
				logger.Error(fmt.Sprintf("Anonymous profile cleanup failed for organization %s", orgID),
					log.Error(err))
				break
			}
			deletedForOrg += deleted
			if deleted < batchSize {
				break
			}
		}
		if deletedForOrg > 0 {
			logger.Info(fmt.Sprintf(
				"Anonymous profile cleanup: deleted %d profiles of organization %s not updated for %d days",
				deletedForOrg, orgID, days))
		}
		total += deletedForOrg
	}
	return total
}
