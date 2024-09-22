package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccPermissionsGroupResource(name string, groupName string) string {
	return fmt.Sprintf(`
resource "metabase_permissions_group" "%s" {
  name        = "%s"
}
`,
		name,
		groupName,
	)
}

func testAccCheckPermissionsGroupExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		groupId, err := strconv.ParseInt(rs.Primary.ID, 10, 64)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetPermissionsGroupWithResponse(context.Background(), int(groupId))
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting permissions group.")
		}

		if rs.Primary.Attributes["name"] != response.JSON200.Name {
			return fmt.Errorf("Terraform resource and API response do not match for permissions group name.")
		}

		return nil
	}
}

func testAccCheckPermissionsGroupDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_permissions_group" {
			continue
		}

		groupId, err := strconv.ParseInt(rs.Primary.ID, 10, 64)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetPermissionsGroupWithResponse(context.Background(), int(groupId))
		if err != nil {
			return err
		}
		if response.StatusCode() == 404 {
			return nil
		}

		return fmt.Errorf("Permissions group %s still exists.", rs.Primary.ID)
	}

	return nil
}

func TestAccPermissionsGroupResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckPermissionsGroupDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccPermissionsGroupResource("test", "👪 Group"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckPermissionsGroupExists("metabase_permissions_group.test"),
					resource.TestCheckResourceAttrSet("metabase_permissions_group.test", "id"),
					resource.TestCheckResourceAttr("metabase_permissions_group.test", "name", "👪 Group"),
				),
			},
			{
				ResourceName: "metabase_permissions_group.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccPermissionsGroupResource("test", "🎁 Updated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_permissions_group.test", "id"),
					resource.TestCheckResourceAttr("metabase_permissions_group.test", "name", "🎁 Updated"),
				),
			},
		},
	})
}
