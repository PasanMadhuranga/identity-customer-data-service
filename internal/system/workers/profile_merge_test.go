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
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// referenceOf returns the status and the parent of a profile's reference row.
func referenceOf(t *testing.T, db *sql.DB, profileID string) (string, string) {
	t.Helper()
	var status string
	var parent sql.NullString
	require.NoError(t, db.QueryRow(`SELECT profile_status, reference_profile_id FROM profile_reference
		WHERE profile_id = ?`, profileID).Scan(&status, &parent))
	return status, parent.String
}

// TestMergeOfAPairAnotherJobMergedIsStale covers two workers that matched overlapping pairs. The
// candidate was merged into another master after this job matched it, so the merge must write
// nothing and report the pair as stale rather than attach the incoming profile to a child.
func TestMergeOfAPairAnotherJobMergedIsStale(t *testing.T) {
	db := candidateWorkerDB(t)
	workerProfile(t, db, "other-master", "same-user", `{"email":"a"}`, 3*time.Hour)
	candidate := workerProfile(t, db, "candidate", "same-user", `{"email":"b"}`, 2*time.Hour)
	incoming := workerProfile(t, db, "incoming", "same-user", `{"email":"c"}`, time.Hour)

	// Another job merges the candidate after this job matched it.
	_, err := db.Exec(`UPDATE profile_reference SET profile_status = 'MERGED_TO',
		reference_profile_id = 'other-master' WHERE profile_id = 'candidate'`)
	require.NoError(t, err)

	master, err := MergeMatchedProfiles(context.Background(), tenantOrg.ID, candidate, incoming, "test")

	require.ErrorIs(t, err, ErrMergeStale)
	require.Nil(t, master)
	status, parent := referenceOf(t, db, "incoming")
	require.Equal(t, "REFERENCE_PROFILE", status)
	require.Empty(t, parent)
}

// TestMergeThatFailsWritesNothing covers a merge that fails after it has rewritten the reference
// rows. Before merges ran in one transaction, the incoming profile stayed a child of a master
// that never received its data, and a second delivery skipped it as a child.
func TestMergeThatFailsWritesNothing(t *testing.T) {
	db := candidateWorkerDB(t)
	candidate := workerProfile(t, db, "candidate", "same-user", `{"email":"a"}`, time.Hour)
	incoming := workerProfile(t, db, "incoming", "same-user", `{"email":"b"}`, 0)

	// Writing the merged attributes to the master fails, after the references are rewritten.
	_, err := db.Exec(`CREATE TRIGGER fail_master_update BEFORE UPDATE ON profiles
		WHEN NEW.profile_id = 'candidate'
		BEGIN SELECT RAISE(ABORT, 'the master cannot be written'); END`)
	require.NoError(t, err)

	master, err := MergeMatchedProfiles(context.Background(), tenantOrg.ID, candidate, incoming, "test")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrMergeStale)
	require.Nil(t, master)
	status, parent := referenceOf(t, db, "incoming")
	require.Equal(t, "REFERENCE_PROFILE", status)
	require.Empty(t, parent)
}

// TestMergeOfAPairStillMerges is the case the transaction must not change: both profiles are
// reference profiles when the merge locks them, so the incoming one becomes a child.
func TestMergeOfAPairStillMerges(t *testing.T) {
	db := candidateWorkerDB(t)
	candidate := workerProfile(t, db, "candidate", "same-user", `{"email":"a"}`, time.Hour)
	incoming := workerProfile(t, db, "incoming", "same-user", `{"email":"b"}`, 0)

	master, err := MergeMatchedProfiles(context.Background(), tenantOrg.ID, candidate, incoming, "test")

	require.NoError(t, err)
	require.NotNil(t, master)
	require.Equal(t, "candidate", master.ProfileID)
	status, parent := referenceOf(t, db, "incoming")
	require.Equal(t, "MERGED_TO", status)
	require.Equal(t, "candidate", parent)
}

// makeChild points a profile at a master, as a merge does.
func makeChild(t *testing.T, db *sql.DB, profileID, masterID string) {
	t.Helper()
	_, err := db.Exec(`UPDATE profile_reference SET profile_status = 'MERGED_TO', reference_profile_id = ?,
		reference_reason = 'earlier' WHERE profile_id = ?`, masterID, profileID)
	require.NoError(t, err)
}

// TestAMasterMatchedAgainTakesTheOtherProfile covers a master that is evaluated again after a
// merge and matches a temporary profile. The master keeps its children and takes the other
// profile as one more, rather than both moving under a new master and leaving the children a
// level below it.
func TestAMasterMatchedAgainTakesTheOtherProfile(t *testing.T) {
	db := candidateWorkerDB(t)
	loner := workerProfile(t, db, "loner", "", `{"email":"a"}`, 2*time.Hour)
	master := workerProfile(t, db, "master", "", `{"email":"a"}`, time.Hour)
	workerProfile(t, db, "kid", "", `{"email":"a"}`, 0)
	makeChild(t, db, "kid", "master")

	survivor, err := MergeMatchedProfiles(context.Background(), tenantOrg.ID, loner, master, "test")

	require.NoError(t, err)
	require.NotNil(t, survivor)
	require.Equal(t, "master", survivor.ProfileID)
	for _, child := range []string{"loner", "kid"} {
		status, parent := referenceOf(t, db, child)
		require.Equal(t, "MERGED_TO", status, child)
		require.Equal(t, "master", parent, child)
	}
}

// TestAMasterDemotedUnderAPermanentProfileHandsOverItsChildren covers a temporary master that
// becomes the child of a permanent profile. Its children move to the permanent profile, so that
// every child points at a reference profile.
func TestAMasterDemotedUnderAPermanentProfileHandsOverItsChildren(t *testing.T) {
	db := candidateWorkerDB(t)
	permanent := workerProfile(t, db, "permanent", "user-1", `{"email":"a"}`, 2*time.Hour)
	master := workerProfile(t, db, "master", "", `{"email":"a"}`, time.Hour)
	workerProfile(t, db, "kid", "", `{"email":"a"}`, 0)
	makeChild(t, db, "kid", "master")

	survivor, err := MergeMatchedProfiles(context.Background(), tenantOrg.ID, permanent, master, "test")

	require.NoError(t, err)
	require.NotNil(t, survivor)
	require.Equal(t, "permanent", survivor.ProfileID)
	for _, child := range []string{"master", "kid"} {
		status, parent := referenceOf(t, db, child)
		require.Equal(t, "MERGED_TO", status, child)
		require.Equal(t, "permanent", parent, child)
	}
	var kidReason string
	require.NoError(t, db.QueryRow(`SELECT reference_reason FROM profile_reference WHERE profile_id = 'kid'`).
		Scan(&kidReason))
	require.Equal(t, "earlier", kidReason, "a moved child keeps the reason it was merged for")
}
