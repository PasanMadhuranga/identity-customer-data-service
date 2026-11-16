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
package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	profileModel "github.com/wso2/identity-customer-data-service/internal/profile/model"
	profileStore "github.com/wso2/identity-customer-data-service/internal/profile/store"
)

func TestProfileIDsByFilterAndRestriction(t *testing.T) {
	env := newProfileEnv(t)
	first, err := env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{UserId: "u-1"}, env.orgID)
	require.NoError(t, err)
	second, err := env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{UserId: "u-2"}, env.orgID)
	require.NoError(t, err)

	ids := func(filters, restrictTo []string) []string {
		got, err := profileStore.GetProfileIDsWithFilters(env.ctx, env.orgID, filters, restrictTo)
		require.NoError(t, err)
		return got
	}

	assert.ElementsMatch(t, []string{first.ProfileId, second.ProfileId}, ids(nil, nil), "no filter lists every master")
	assert.ElementsMatch(t, []string{first.ProfileId}, ids([]string{"user_id eq u-1"}, nil))
	assert.ElementsMatch(t, []string{second.ProfileId}, ids([]string{"profile_id eq " + second.ProfileId}, nil))
	assert.Empty(t, ids([]string{"user_id eq nobody"}, nil))
	assert.ElementsMatch(t, []string{second.ProfileId}, ids(nil, []string{second.ProfileId}),
		"the result is limited to the profiles it is restricted to")
	assert.Empty(t, ids([]string{"user_id eq u-1"}, []string{second.ProfileId}))
	assert.ElementsMatch(t, []string{first.ProfileId, second.ProfileId}, ids([]string{"malformed", "traits.city gt 5"},
		nil),
		"a condition the builder cannot read is left out")
}

func TestProfileIDsMatchEveryFilterOperator(t *testing.T) {
	env := newProfileEnv(t)
	env.seedSchemaForApp(t, "app-1")
	alice := env.create(t, profileModel.ProfileRequest{
		UserId:          "alice-1",
		Traits:          map[string]interface{}{"city": "Colombo"},
		ApplicationData: map[string]interface{}{"app-1": map[string]interface{}{"theme": "dark"}},
	})
	bob := env.create(t, profileModel.ProfileRequest{
		UserId:          "bob-2",
		Traits:          map[string]interface{}{"city": "Kandy"},
		ApplicationData: map[string]interface{}{"app-1": map[string]interface{}{"theme": "light"}},
	})
	carol := env.create(t, profileModel.ProfileRequest{
		UserId:          "carol-3",
		Traits:          map[string]interface{}{"city": "Colombo"},
		ApplicationData: map[string]interface{}{"app-1": map[string]interface{}{"theme": "dark"}},
	})

	ids := func(filters ...string) []string {
		got, err := profileStore.GetProfileIDsWithFilters(env.ctx, env.orgID, filters, nil)
		require.NoError(t, err)
		return got
	}
	colombo := []string{alice.ProfileId, carol.ProfileId}

	assert.ElementsMatch(t, colombo, ids("traits.city eq Colombo"), "trait equality")
	assert.ElementsMatch(t, colombo, ids("traits.city co olo"), "trait contains")
	assert.ElementsMatch(t, colombo, ids("traits.city co COLOM"), "a trait search ignores case")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("traits.city sw Kan"), "trait starts with")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("traits.city ew ndy"), "trait ends with")
	assert.Empty(t, ids("traits.city eq colombo"), "equality is exact")

	assert.ElementsMatch(t, []string{alice.ProfileId}, ids("user_id eq alice-1"))
	assert.ElementsMatch(t, []string{alice.ProfileId}, ids("user_id co ice"), "user id contains")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("user_id sw bob"), "user id starts with")
	assert.ElementsMatch(t, []string{carol.ProfileId}, ids("user_id ew -3"), "user id ends with")

	assert.ElementsMatch(t, []string{alice.ProfileId}, ids("profile_id sw "+alice.ProfileId[:8]),
		"profile id starts with")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("profile_id ew "+bob.ProfileId[len(bob.ProfileId)-8:]),
		"profile id ends with")
	assert.ElementsMatch(t, []string{carol.ProfileId}, ids("profile_id co "+carol.ProfileId[8:16]),
		"profile id contains")

	dark := []string{alice.ProfileId, carol.ProfileId}
	assert.ElementsMatch(t, dark, ids("application_data.app-1.theme eq dark"), "application data equality")
	assert.ElementsMatch(t, dark, ids("application_data.app-1.theme co ar"), "application data contains")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("application_data.app-1.theme sw li"),
		"application data starts with")
	assert.ElementsMatch(t, []string{bob.ProfileId}, ids("application_data.app-1.theme ew ht"),
		"application data ends with")
	assert.ElementsMatch(t, dark, ids("application_data.theme eq dark"),
		"application data of any application")

	assert.ElementsMatch(t, []string{alice.ProfileId}, ids("traits.city eq Colombo", "user_id sw alice"),
		"filters combine")
	assert.ElementsMatch(t, []string{alice.ProfileId},
		ids("traits.city eq Colombo", "application_data.app-1.theme eq dark", "user_id co lic"),
		"filters over different fields combine")

	restricted, err := profileStore.GetProfileIDsWithFilters(env.ctx, env.orgID,
		[]string{"traits.city eq Colombo"}, []string{carol.ProfileId, bob.ProfileId})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{carol.ProfileId}, restricted, "the filters apply within the restriction")
}

func TestProfileIDsOfAnotherOrganizationAreNotListed(t *testing.T) {
	env := newProfileEnv(t)
	env.createWithCity(t, "Colombo")

	got, err := profileStore.GetProfileIDsWithFilters(env.ctx, "another-org", nil, nil)

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestProfileIDsRefuseAFieldThatCouldBreakTheQuery(t *testing.T) {
	env := newProfileEnv(t)

	_, err := profileStore.GetProfileIDsWithFilters(env.ctx, env.orgID,
		[]string{"traits.city'; DROP TABLE profiles; -- eq x"}, nil)
	_ = requireStatus(t, err, 400)
	_, _, err = profileStore.GetAllProfilesWithFilter(env.ctx, env.orgID, []string{"traits.a'b eq x"}, 10, nil)
	_ = requireStatus(t, err, 400)
}

func TestMergedMasterDataIsWrittenToTheMaster(t *testing.T) {
	env := newProfileEnv(t)
	env.seedSchemaForApp(t, "app-1")
	master, err := env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{
		Traits:          map[string]interface{}{"city": "Colombo"},
		ApplicationData: map[string]interface{}{"app-1": map[string]interface{}{"theme": "dark"}},
	}, env.orgID)
	require.NoError(t, err)

	require.NoError(t, profileStore.InsertMergedMasterProfileTraitData(env.ctx, master.ProfileId, env.orgID,
		map[string]interface{}{"city": "Kandy", "tier": "gold"}))
	require.NoError(t, profileStore.MergeIdentityDataOfProfiles(env.ctx, master.ProfileId, env.orgID,
		map[string]interface{}{"email": "a@b.c"}))
	require.NoError(t, profileStore.InsertMergedMasterProfileAppData(env.ctx, master.ProfileId, env.orgID,
		profileModel.ApplicationData{AppId: "app-1", AppSpecificData: map[string]interface{}{"lang": "en"}}))
	require.NoError(t, profileStore.InsertMergedMasterProfileAppData(env.ctx, master.ProfileId, env.orgID,
		profileModel.ApplicationData{AppId: "app-2", AppSpecificData: map[string]interface{}{"theme": "light"}}))

	stored, err := profileStore.GetProfile(env.ctx, master.ProfileId, env.orgID)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"city": "Kandy", "tier": "gold"}, stored.Traits, "the traits are replaced")
	assert.Equal(t, "a@b.c", stored.IdentityAttributes["email"])
	apps := ConvertAppDataToMap(stored.ApplicationData)
	assert.Equal(t, map[string]interface{}{"theme": "dark", "lang": "en"}, apps["app-1"],
		"the data of an application is merged")
	assert.Equal(t, map[string]interface{}{"theme": "light"}, apps["app-2"], "a new application is added")
}

func TestMergedMasterWritesRefuseAnUnknownProfile(t *testing.T) {
	env := newProfileEnv(t)

	assert.Error(t, profileStore.InsertMergedMasterProfileTraitData(env.ctx, "missing", env.orgID, nil))
	assert.Error(t, profileStore.MergeIdentityDataOfProfiles(env.ctx, "missing", env.orgID, nil))
	assert.Error(t, profileStore.InsertMergedMasterProfileAppData(env.ctx, "missing", env.orgID,
		profileModel.ApplicationData{AppId: "app-1"}))
}

func TestProfilesAreRepointedAtAMasterAndDetached(t *testing.T) {
	env := newProfileEnv(t)
	master := env.createWithCity(t, "Colombo")
	first := env.createWithCity(t, "Kandy")
	second := env.createWithCity(t, "Galle")

	require.NoError(t, profileStore.UpdateProfileReferences(env.ctx, profileModel.Profile{ProfileId: master.ProfileId},
		[]profileModel.Reference{{ProfileId: first.ProfileId, Reason: "email"}, {ProfileId: second.ProfileId,
			Reason: "phone"}}))

	children, err := profileStore.FetchReferencedProfiles(env.ctx, master.ProfileId)
	require.NoError(t, err)
	reasons := map[string]string{}
	for _, child := range children {
		reasons[child.ProfileId] = child.Reason
	}
	assert.Equal(t, map[string]string{first.ProfileId: "email", second.ProfileId: "phone"}, reasons)
	merged, err := env.svc.GetProfile(env.ctx, first.ProfileId, env.orgID)
	require.NoError(t, err)
	require.NotNil(t, merged.MergedTo)
	assert.Equal(t, master.ProfileId, merged.MergedTo.ProfileId)

	require.NoError(t, profileStore.DetachRefererProfileFromReference(env.ctx, master.ProfileId, first.ProfileId))
	children, err = profileStore.FetchReferencedProfiles(env.ctx, master.ProfileId)
	require.NoError(t, err)
	require.Len(t, children, 1)
	assert.Equal(t, second.ProfileId, children[0].ProfileId)
}

func TestProfileApplicationDataIsReadInSeveralWays(t *testing.T) {
	env := newProfileEnv(t)
	env.seedSchemaForApp(t, "app-1")
	env.seedSchemaForApp(t, "app-2")
	withApps, err := env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{
		ApplicationData: map[string]interface{}{
			"app-1": map[string]interface{}{"theme": "dark"},
			"app-2": map[string]interface{}{"theme": "light"},
		},
	}, env.orgID)
	require.NoError(t, err)
	without := env.createWithCity(t, "Colombo")

	withData, err := profileStore.GetProfileWithOptions(env.ctx, withApps.ProfileId, env.orgID,
		profileStore.GetProfileOptions{IncludeApplicationData: true})
	require.NoError(t, err)
	assert.Len(t, withData.ApplicationData, 2)
	withoutData, err := profileStore.GetProfileWithOptions(env.ctx, withApps.ProfileId, env.orgID,
		profileStore.GetProfileOptions{IncludeApplicationData: false})
	require.NoError(t, err)
	assert.Empty(t, withoutData.ApplicationData, "the data of the applications is read only when asked for")

	one, err := profileStore.FetchApplicationDataWithAppId(env.ctx, withApps.ProfileId, "app-2")
	require.NoError(t, err)
	assert.Equal(t, "app-2", one.AppId)
	assert.Equal(t, "light", one.AppSpecificData["theme"])

	batch, err := profileStore.FetchApplicationDataBatch(env.ctx, []string{withApps.ProfileId, without.ProfileId})
	require.NoError(t, err)
	assert.Len(t, batch[withApps.ProfileId], 2)
	assert.Empty(t, batch[without.ProfileId])
	empty, err := profileStore.FetchApplicationDataBatch(env.ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestAProfileIsFoundByItsUserID(t *testing.T) {
	env := newProfileEnv(t)
	created, err := env.svc.CreateProfile(env.ctx, profileModel.ProfileRequest{UserId: "u-1"}, env.orgID)
	require.NoError(t, err)

	found, err := profileStore.GetProfileWithUserId(env.ctx, "u-1", env.orgID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ProfileId, found.ProfileId)

	none, err := profileStore.GetProfileWithUserId(env.ctx, "nobody", env.orgID)
	require.NoError(t, err)
	assert.Nil(t, none)
	otherOrg, err := profileStore.GetProfileWithUserId(env.ctx, "u-1", "another-org")
	require.NoError(t, err)
	assert.Nil(t, otherOrg)
}
