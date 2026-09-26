package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccUserDataSource() string {
	return `
data "metabase_user" "by_id" {
  id = metabase_user.source.id
}

data "metabase_user" "by_email" {
  email = metabase_user.source.email
}
`
}

func TestAccUserDataSource(t *testing.T) {
	email := testAccUserEmail("terraform-data-source")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckUserDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig +
					testAccUserResource("source", email, "Data", "Source", "") +
					testAccUserDataSource(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.metabase_user.by_id", "id", "metabase_user.source", "id"),
					resource.TestCheckResourceAttr("data.metabase_user.by_id", "email", email),
					resource.TestCheckResourceAttr("data.metabase_user.by_id", "common_name", "Data Source"),
					resource.TestCheckResourceAttr("data.metabase_user.by_id", "is_active", "true"),
					resource.TestCheckResourceAttr("data.metabase_user.by_id", "is_superuser", "false"),
					resource.TestCheckResourceAttrPair("data.metabase_user.by_email", "id", "metabase_user.source", "id"),
					resource.TestCheckResourceAttr("data.metabase_user.by_email", "first_name", "Data"),
					resource.TestCheckResourceAttr("data.metabase_user.by_email", "last_name", "Source"),
				),
			},
		},
	})
}
