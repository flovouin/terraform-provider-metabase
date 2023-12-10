package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccDatabaseResource(name string, dbName string) string {
	return fmt.Sprintf(`
resource "metabase_database" "%s" {
  name = "%s"

  custom_details = {
    engine = "postgres"

    details_json = jsonencode({
      host                    = "%s"
      port                    = 5432
      dbname                  = "%s"
      user                    = "%s"
      password                = "%s"
      schema-filters-type     = "inclusion"
      schema-filters-patterns = "this_schema_only"
      ssl                     = false
      tunnel-enabled          = false
      advanced-options        = false
    })

    redacted_attributes = [
      "password",
    ]
  }
}
`,
		name,
		dbName,
		os.Getenv("PG_HOST"),
		os.Getenv("PG_DATABASE"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
	)
}

func testAccCheckDatabaseExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetDatabaseWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting database.")
		}

		if rs.Primary.Attributes["name"] != response.JSON200.Name {
			return fmt.Errorf("Terraform resource and API response do not match for database name.")
		}

		return nil
	}
}

func testAccCheckDatabaseDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_database" {
			continue
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetDatabaseWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 404 {
			return fmt.Errorf("Database %s still exists.", rs.Primary.ID)
		}
	}

	return nil
}

func TestAccDatabaseResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccDatabaseResource("test", "🐘 PG"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseExists("metabase_database.test"),
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "🐘 PG"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
				),
			},
			{
				ResourceName: "metabase_database.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccDatabaseResource("test", "✨ New"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "✨ New"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
				),
			},
		},
	})
}
