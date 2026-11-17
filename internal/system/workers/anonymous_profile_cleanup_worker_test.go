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
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wso2/identity-customer-data-service/internal/system/log"
)

type deleteCall struct {
	orgID  string
	cutoff time.Time
	size   int
}

// fakeSource stands in for the database. backlog is how many expired profiles each organization holds.
type fakeSource struct {
	orgIDs      []string
	retentions  map[string]int
	primaries   map[string]string
	backlog     map[string]int
	orgsErr     error
	retentErr   error
	primariesEr error
	failFor     map[string]error
	calls       []deleteCall
}

func (f *fakeSource) OrgIDs(context.Context) ([]string, error) { return f.orgIDs, f.orgsErr }

func (f *fakeSource) Retentions(context.Context) (map[string]int, error) {
	return f.retentions, f.retentErr
}

func (f *fakeSource) Primaries(context.Context) (map[string]string, error) {
	return f.primaries, f.primariesEr
}

func (f *fakeSource) DeleteExpired(_ context.Context, orgID string, cutoff time.Time, size int) (int, error) {
	f.calls = append(f.calls, deleteCall{orgID, cutoff, size})
	if err := f.failFor[orgID]; err != nil {
		return 0, err
	}
	deleted := min(f.backlog[orgID], size)
	f.backlog[orgID] -= deleted
	return deleted, nil
}

func TestRetentionDaysForUsesTheOrganizationThenItsPrimaryThenTheDefault(t *testing.T) {
	retentions := map[string]int{"acme": 30, "globex": 200}
	primaries := map[string]string{"acme": "acme", "acme-sub": "acme", "globex": "globex", "initech": "initech"}

	assert.Equal(t, 30, retentionDaysFor("acme", retentions, primaries, 90), "an organization uses its own setting")
	assert.Equal(t, 30, retentionDaysFor("acme-sub", retentions, primaries, 90),
		"a sub organization follows its primary organization")
	assert.Equal(t, 90, retentionDaysFor("initech", retentions, primaries, 90),
		"an organization with no setting uses the default")
	assert.Equal(t, 90, retentionDaysFor("unregistered", retentions, primaries, 90),
		"an organization that is not stored uses the default")
	ownRow := map[string]int{"acme-sub": 45}
	assert.Equal(t, 90, retentionDaysFor("acme-sub", ownRow, map[string]string{"acme-sub": "acme"}, 90),
		"a sub organization cannot set a retention, so a row of its own is ignored")
}

func TestCleanupAppliesEachOrganizationsRetention(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		orgIDs:     []string{"acme", "globex"},
		retentions: map[string]int{"acme": 30},
		primaries:  map[string]string{},
		backlog:    map[string]int{"acme": 3, "globex": 2},
	}

	total := runAnonymousProfileCleanup(context.Background(), source, 90, 500, now)

	assert.Equal(t, 5, total)
	require.Len(t, source.calls, 2)
	assert.Equal(t, now.Add(-30*24*time.Hour), source.calls[0].cutoff, "acme keeps profiles for its own 30 days")
	assert.Equal(t, now.Add(-90*24*time.Hour), source.calls[1].cutoff, "globex gets the 90 day default")
}

func TestCleanupRunsBatchesUntilTheBacklogIsGone(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	source := &fakeSource{
		orgIDs:    []string{"acme"},
		primaries: map[string]string{},
		backlog:   map[string]int{"acme": 5},
	}

	total := runAnonymousProfileCleanup(context.Background(), source, 90, 2, time.Now())

	assert.Equal(t, 5, total)
	assert.Len(t, source.calls, 3, "batches of 2, 2 and 1; the short batch ends the run")
}

func TestCleanupStopsAnOrganizationAtItsBatchLimit(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	source := &fakeSource{
		orgIDs:    []string{"acme", "globex"},
		primaries: map[string]string{},
		backlog:   map[string]int{"acme": 1 << 30, "globex": 1},
	}

	runAnonymousProfileCleanup(context.Background(), source, 90, 1, time.Now())

	acme := 0
	for _, call := range source.calls {
		if call.orgID == "acme" {
			acme++
		}
	}
	assert.Equal(t, maxAnonymousCleanupBatchesPerOrg, acme, "one organization cannot hold the run indefinitely")
	assert.Equal(t, 0, source.backlog["globex"], "the next organization is still served")
}

func TestCleanupContinuesPastAFailingOrganization(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	source := &fakeSource{
		orgIDs:    []string{"acme", "globex"},
		primaries: map[string]string{},
		backlog:   map[string]int{"globex": 2},
		failFor:   map[string]error{"acme": errors.New("boom")},
	}

	total := runAnonymousProfileCleanup(context.Background(), source, 90, 500, time.Now())

	assert.Equal(t, 2, total)
}

func TestCleanupDeletesNothingWhenItCannotReadTheSettings(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	for name, source := range map[string]*fakeSource{
		"organizations": {orgsErr: errors.New("down")},
		"retentions":    {orgIDs: []string{"acme"}, retentErr: errors.New("down")},
		"primaries":     {orgIDs: []string{"acme"}, primariesEr: errors.New("down")},
	} {
		t.Run(name, func(t *testing.T) {
			source.backlog = map[string]int{"acme": 3}

			total := runAnonymousProfileCleanup(context.Background(), source, 90, 500, time.Now())

			assert.Zero(t, total)
			assert.Empty(t, source.calls, "a shorter retention could be overridden by the default, so nothing is deleted")
		})
	}
}

func TestCleanupStopsWhenTheContextEnds(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &fakeSource{orgIDs: []string{"acme"}, primaries: map[string]string{}, backlog: map[string]int{"acme": 3}}

	total := runAnonymousProfileCleanup(ctx, source, 90, 500, time.Now())

	assert.Zero(t, total)
	assert.Empty(t, source.calls)
}

func TestStopAnonymousProfileCleanupWorkerIsSafeWithoutAStart(t *testing.T) {
	require.NoError(t, StopAnonymousProfileCleanupWorker(context.Background()))
}

func TestCleanupSkipsAnOrganizationWhoseRetentionOutlastsEveryProfile(t *testing.T) {
	require.NoError(t, log.Init("ERROR"))
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	source := &fakeSource{
		orgIDs:     []string{"forever", "acme"},
		retentions: map[string]int{"forever": math.MaxInt32, "acme": maxMeaningfulRetentionDays},
		primaries:  map[string]string{},
		backlog:    map[string]int{"forever": 5, "acme": 1},
	}

	total := runAnonymousProfileCleanup(context.Background(), source, 90, 500, now)

	assert.Equal(t, 1, total)
	require.Len(t, source.calls, 1, "an organization with an unreachable cutoff is not queried")
	assert.Equal(t, "acme", source.calls[0].orgID)
	assert.Equal(t, 5, source.backlog["forever"])
}
