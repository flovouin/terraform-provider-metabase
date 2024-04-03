package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var (
	existingTableName   = "ACCOUNTS"
	expectedDisplayName = "Accounts"
	expectedDescription = "Information on customer accounts registered with Piespace. Each account represents a new organization signing up for on-demand pies."
	newDisplayName      = "🏦 Accounts"
)

func testAccTableResource(name string, tableName string, displayName *string) string {
	var displayNameAttribute = ""
	if displayName != nil {
		displayNameAttribute = fmt.Sprintf(`display_name = "%s"`, *displayName)
	}
	// This references the sample database, which should always have ID 1.
	return fmt.Sprintf(`
resource "metabase_table" "%s" {
  db_id = 1
  name  = "%s"

	%s
}
`,
		name,
		tableName,
		displayNameAttribute,
	)
}

func TestAccTableResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccTableResource("test", existingTableName, nil),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_table.test", "id", "6"),
					resource.TestCheckResourceAttr("metabase_table.test", "schema", "PUBLIC"),
					resource.TestCheckResourceAttr("metabase_table.test", "display_name", expectedDisplayName),
					resource.TestCheckResourceAttr("metabase_table.test", "description", expectedDescription),
				),
			},
			{
				ResourceName: "metabase_table.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccTableResource("test", existingTableName, &newDisplayName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("metabase_table.test", "id", "6"),
					resource.TestCheckResourceAttr("metabase_table.test", "schema", "PUBLIC"),
					resource.TestCheckResourceAttr("metabase_table.test", "display_name", newDisplayName),
					resource.TestCheckResourceAttr("metabase_table.test", "description", expectedDescription),
				),
			},
			{
				// Just to set the original display name back.
				Config: providerConfig + testAccTableResource("test", existingTableName, &expectedDisplayName),
			},
		},
	})
}
