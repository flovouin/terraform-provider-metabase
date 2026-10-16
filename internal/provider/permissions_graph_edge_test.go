package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Returns the permissions of the given group on the given database, as returned by the Metabase API.
func testAccGetDatabasePermissions(groupId string, databaseId string) (*metabase.PermissionsGraphDatabasePermissions, error) {
	response, err := testAccMetabaseClient.GetPermissionsGraphWithResponse(context.Background())
	if err != nil {
		return nil, err
	}
	if response.StatusCode() != 200 {
		return nil, fmt.Errorf("Received unexpected response from the Metabase API when getting the permissions graph.")
	}

	permissions, ok := response.JSON200.Groups[groupId][databaseId]
	if !ok {
		return nil, fmt.Errorf("The permissions graph contains no permissions for group %s and database %s.", groupId, databaseId)
	}

	return &permissions, nil
}

// Checks that the permissions of the given group on the given database have been revoked in Metabase.
func testAccCheckRevokedDatabasePermissions(groupId string, databaseId func(*terraform.State) (string, error)) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		dbId, err := databaseId(s)
		if err != nil {
			return err
		}

		permissions, err := testAccGetDatabasePermissions(groupId, dbId)
		if err != nil {
			return err
		}

		if !permissions.IsRevoked() {
			b, _ := json.Marshal(permissions)
			return fmt.Errorf("Expected the permissions of group %s on database %s to be revoked, got: %s.", groupId, dbId, b)
		}

		return nil
	}
}
