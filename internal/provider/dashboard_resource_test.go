package provider

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testAccDashboardResource(name string, dashboardName string, description string) string {
	return fmt.Sprintf(`
resource "metabase_dashboard" "%s" {
  name        = "%s"
  description = "%s"

  parameters_json = jsonencode([
    {
      id        = "83e68ca2"
      name      = "Date range"
      slug      = "date_filter"
      type      = "date/all-options"
      sectionId = "date"
      default   = "past30days"
    },
  ])

  cards_json = jsonencode([
    {
      card_id = null
      col     = 0
      row     = 0
      sizeX   = 7
      sizeY   = 6
      series  = []
      visualization_settings = {
        virtual_card = {
          name                   = null
          display                = "text"
          visualization_settings = {}
          dataset_query          = {}
          archived               = false
        },
        text                  = "# ❗️ Some catchy title\n\nIsn't this great?"
        "dashcard.background" = false
      }
      parameter_mappings = []
    }
  ])
}
`,
		name,
		dashboardName,
		description,
	)
}

func testAccCheckDashboardExists(resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("Failed to find resource %s in state.", resourceName)
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetDashboardWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 200 {
			return fmt.Errorf("Received unexpected response from the Metabase API when getting dashboard.")
		}

		if rs.Primary.Attributes["name"] != response.JSON200.Name {
			return fmt.Errorf("Terraform resource and API response do not match for dashboard name.")
		}

		return nil
	}
}

func testAccCheckDashboardDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "metabase_dashboard" {
			continue
		}

		id, err := strconv.Atoi(rs.Primary.ID)
		if err != nil {
			return err
		}

		response, err := testAccMetabaseClient.GetDashboardWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		if response.StatusCode() != 404 {
			return fmt.Errorf("Dashboard %s still exists.", rs.Primary.ID)
		}
	}

	return nil
}

func TestAccDashboardResource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDashboardDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccDashboardResource("test", "📈 Dashboard", "📖 Description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardExists("metabase_dashboard.test"),
					resource.TestCheckResourceAttrSet("metabase_dashboard.test", "id"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "name", "📈 Dashboard"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "description", "📖 Description"),
				),
			},
			{
				ResourceName: "metabase_dashboard.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccDashboardResource("test", "📉 Updated", "📕 Updated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_dashboard.test", "id"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "name", "📉 Updated"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "description", "📕 Updated"),
				),
			},
		},
	})
}
