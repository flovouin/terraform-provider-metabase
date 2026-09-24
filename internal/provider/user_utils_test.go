package provider

import (
	"context"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func makeGroupIdsSet(ids ...int64) types.Set {
	elements := make([]attr.Value, 0, len(ids))
	for _, id := range ids {
		elements = append(elements, types.Int64Value(id))
	}

	return types.SetValueMust(types.Int64Type, elements)
}

func TestManagedGroupIdsFromUserGroupMemberships(t *testing.T) {
	groupIds, ok := managedGroupIdsFromUser(metabase.User{
		Id:    1,
		Email: "user@tests.com",
		UserGroupMemberships: &[]metabase.UserGroupMembership{
			{Id: metabase.AllUsersPermissionsGroupId},
			{Id: metabase.AdministratorsPermissionsGroupId},
			{Id: 5},
		},
	})

	if !ok {
		t.Fatal("Expected the group IDs to be reported by the Metabase API.")
	}
	if len(groupIds) != 1 || groupIds[0] != 5 {
		t.Fatalf("Expected the groups managed by Metabase to be filtered out, got %v.", groupIds)
	}
}

func TestManagedGroupIdsFromUserGroupIds(t *testing.T) {
	groupIds, ok := managedGroupIdsFromUser(metabase.User{
		Id:       1,
		Email:    "user@tests.com",
		GroupIds: &[]int{metabase.AllUsersPermissionsGroupId, 7},
	})

	if !ok {
		t.Fatal("Expected the group IDs to be reported by the Metabase API.")
	}
	if len(groupIds) != 1 || groupIds[0] != 7 {
		t.Fatalf("Expected the groups managed by Metabase to be filtered out, got %v.", groupIds)
	}
}

func TestManagedGroupIdsWhenNotReturnedByTheApi(t *testing.T) {
	if _, ok := managedGroupIdsFromUser(metabase.User{Id: 1, Email: "user@tests.com"}); ok {
		t.Fatal("Expected the groups to be reported as unknown when the Metabase API does not return them.")
	}
}

func TestMakeUserGroupMembershipsAlwaysContainsAllUsers(t *testing.T) {
	memberships, diags := makeUserGroupMemberships(context.Background(), makeGroupIdsSet(5), types.BoolValue(false))
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}

	expected := map[int]bool{metabase.AllUsersPermissionsGroupId: true, 5: true}
	if len(memberships) != len(expected) {
		t.Fatalf("Expected %d memberships, got %v.", len(expected), memberships)
	}
	for _, m := range memberships {
		if !expected[m.Id] {
			t.Fatalf("Unexpected membership for group %d.", m.Id)
		}
	}
}

func TestMakeUserGroupMembershipsContainsAdministratorsForSuperusers(t *testing.T) {
	memberships, diags := makeUserGroupMemberships(context.Background(), types.SetNull(types.Int64Type), types.BoolValue(true))
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}

	expected := map[int]bool{metabase.AllUsersPermissionsGroupId: true, metabase.AdministratorsPermissionsGroupId: true}
	if len(memberships) != len(expected) {
		t.Fatalf("Expected %d memberships, got %v.", len(expected), memberships)
	}
	for _, m := range memberships {
		if !expected[m.Id] {
			t.Fatalf("Unexpected membership for group %d.", m.Id)
		}
	}
}
