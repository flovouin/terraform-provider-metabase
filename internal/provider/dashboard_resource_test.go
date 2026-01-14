package provider

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestSortDashcards(t *testing.T) {
	tests := []struct {
		name     string
		input    []any
		expected []any
	}{
		{
			name: "sorts by row then col",
			input: []any{
				map[string]any{"card_id": float64(2), "row": float64(1), "col": float64(0)},
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
				map[string]any{"card_id": float64(3), "row": float64(0), "col": float64(5)},
			},
			expected: []any{
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
				map[string]any{"card_id": float64(3), "row": float64(0), "col": float64(5)},
				map[string]any{"card_id": float64(2), "row": float64(1), "col": float64(0)},
			},
		},
		{
			name: "sorts by tab_id first",
			input: []any{
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0), "dashboard_tab_id": float64(2)},
				map[string]any{"card_id": float64(2), "row": float64(0), "col": float64(0), "dashboard_tab_id": float64(1)},
				map[string]any{"card_id": float64(3), "row": float64(1), "col": float64(0), "dashboard_tab_id": float64(1)},
			},
			expected: []any{
				map[string]any{"card_id": float64(2), "row": float64(0), "col": float64(0), "dashboard_tab_id": float64(1)},
				map[string]any{"card_id": float64(3), "row": float64(1), "col": float64(0), "dashboard_tab_id": float64(1)},
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0), "dashboard_tab_id": float64(2)},
			},
		},
		{
			name: "handles null card_id (text cards)",
			input: []any{
				map[string]any{"card_id": nil, "row": float64(5), "col": float64(0)},
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
				map[string]any{"card_id": nil, "row": float64(0), "col": float64(6)},
			},
			expected: []any{
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
				map[string]any{"card_id": nil, "row": float64(0), "col": float64(6)},
				map[string]any{"card_id": nil, "row": float64(5), "col": float64(0)},
			},
		},
		{
			name:     "handles empty slice",
			input:    []any{},
			expected: []any{},
		},
		{
			name: "handles single element",
			input: []any{
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
			},
			expected: []any{
				map[string]any{"card_id": float64(1), "row": float64(0), "col": float64(0)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying test data
			input := make([]any, len(tt.input))
			copy(input, tt.input)

			sortDashcards(input)

			if !reflect.DeepEqual(input, tt.expected) {
				t.Errorf("sortDashcards() = %v, want %v", input, tt.expected)
			}
		})
	}
}

func testAccDashboardResource(name string, dashboardName string, description string) string {
	return fmt.Sprintf(`
resource "metabase_dashboard" "%s" {
  name        = "%s"
  description = "%s"

  parameters_json = jsonencode([
    {
      "name": "Month and Year",
      "slug": "month_and_year",
      "id": "fb55bed",
      "type": "date/month-year",
      "sectionId": "date",
      "required": true,
      "default": "2024-02"
    },
    {
      "name": "Text",
      "slug": "text",
      "id": "dac08e9",
      "type": "string/=",
      "sectionId": "string",
      "filteringParameters": [
        "fb55bed"
      ],
    },
    {
      "name": "Text",
      "slug": "text",
      "id": "cba622a",
      "type": "string/=",
      "sectionId": "string",
      "values_query_type": "list",
      "values_source_config": {
        "values": ["foo", "bar"]
      },
      "values_source_type": "static-list"
    }
  ])

  cards_json = jsonencode([
    {
      card_id = null
      col = 0
      row = 0
      size_x = 6
      size_y = 3
      series = []
      visualization_settings = {
        virtual_card = {
          name = null
          display = "text"
          visualization_settings = {}
          dataset_query = {}
          archived = false
        }
        text = "🎉"
      }
      parameter_mappings = []
    },
    {
      card_id = null
      col = 6
      row = 0
      size_x = 6
      size_y = 3
      series = []
      parameter_mappings = []
      visualization_settings = {
        virtual_card = {
          name = null
          display = "text"
          visualization_settings = {}
          dataset_query = {}
          archived = false
        }
        text = "🐶"
      }
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
		if response.StatusCode() != 404 && !response.JSON200.Archived {
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
				Config: providerApiKeyConfig + testAccDashboardResource("test", "📈 Dashboard", "📖 Description"),
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
				Config: providerApiKeyConfig + testAccDashboardResource("test", "📉 Updated", "📕 Updated"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("metabase_dashboard.test", "id"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "name", "📉 Updated"),
					resource.TestCheckResourceAttr("metabase_dashboard.test", "description", "📕 Updated"),
				),
			},
		},
	})
}

func testAccDashboardResourceWithTabs(name string, dashboardName string, description string, extraCard bool) string {
	// Extra card for Tab 1 - inserted in sorted position (after Tab 1 col=0, before Tab 2)
	extraCardJson := ""
	if extraCard {
		extraCardJson = `
    {
      card_id = null
      col = 6
      row = 0
      size_x = 6
      size_y = 3
      series = []
      parameter_mappings = []
      visualization_settings = {
        virtual_card = {
          name = null
          display = "text"
          visualization_settings = {}
          dataset_query = {}
          archived = false
        }
        text = "Extra card on Tab 1"
      }
      dashboard_tab_id = 1
    },`
	}

	return fmt.Sprintf(`
resource "metabase_dashboard" "%s" {
  name        = "%s"
  description = "%s"

  tabs_json = jsonencode([
    {
      "id": 1,
      "name": "Tab 1"
    },
    {
      "id": 2,
      "name": "Tab 2"
    }
  ])

  cards_json = jsonencode([
    {
      card_id = null
      col = 0
      row = 0
      size_x = 6
      size_y = 3
      series = []
      visualization_settings = {
        virtual_card = {
          name = null
          display = "text"
          visualization_settings = {}
          dataset_query = {}
          archived = false
        }
        text = "Content on Tab 1"
      }
      parameter_mappings = []
      dashboard_tab_id = 1
    },%s
    {
      card_id = null
      col = 0
      row = 0
      size_x = 6
      size_y = 3
      series = []
      parameter_mappings = []
      visualization_settings = {
        virtual_card = {
          name = null
          display = "text"
          visualization_settings = {}
          dataset_query = {}
          archived = false
        }
        text = "Content on Tab 2"
      }
      dashboard_tab_id = 2
    }
  ])
}
`,
		name,
		dashboardName,
		description,
		extraCardJson,
	)
}

func TestAccDashboardResourceWithTabs(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDashboardDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerApiKeyConfig + testAccDashboardResourceWithTabs("test_tabs", "Dashboard with Tabs", "A dashboard with tabs", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardExists("metabase_dashboard.test_tabs"),
					resource.TestCheckResourceAttrSet("metabase_dashboard.test_tabs", "id"),
					resource.TestCheckResourceAttr("metabase_dashboard.test_tabs", "name", "Dashboard with Tabs"),
					resource.TestCheckResourceAttrSet("metabase_dashboard.test_tabs", "tabs_json"),
				),
			},
			{
				ResourceName: "metabase_dashboard.test_tabs",
				ImportState:  true,
			},
			// Update: add an extra card to Tab 1
			{
				Config: providerApiKeyConfig + testAccDashboardResourceWithTabs("test_tabs", "Dashboard with Tabs", "A dashboard with tabs", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDashboardExists("metabase_dashboard.test_tabs"),
					resource.TestCheckResourceAttrSet("metabase_dashboard.test_tabs", "id"),
					resource.TestCheckResourceAttr("metabase_dashboard.test_tabs", "name", "Dashboard with Tabs"),
					resource.TestCheckResourceAttrSet("metabase_dashboard.test_tabs", "tabs_json"),
				),
			},
		},
	})
}
