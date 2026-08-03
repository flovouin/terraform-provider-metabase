package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testCustomDetailsValue(detailsJson string, sensitiveDetailsJsonWo types.String, sensitiveDetailsJsonWoVersion types.Int64, redactedAttributes ...string) types.Object {
	redactedAttributesValue := types.SetNull(types.StringType)
	if len(redactedAttributes) > 0 {
		attributeValues := make([]attr.Value, 0, len(redactedAttributes))
		for _, attribute := range redactedAttributes {
			attributeValues = append(attributeValues, types.StringValue(attribute))
		}
		redactedAttributesValue = types.SetValueMust(types.StringType, attributeValues)
	}

	return types.ObjectValueMust(customDetailsObjectType.AttrTypes, map[string]attr.Value{
		"engine":                            types.StringValue("postgres"),
		"details_json":                      types.StringValue(detailsJson),
		"sensitive_details_json_wo":         sensitiveDetailsJsonWo,
		"sensitive_details_json_wo_version": sensitiveDetailsJsonWoVersion,
		"redacted_attributes":               redactedAttributesValue,
	})
}

func TestMakeEngineAndDetailsFromModelCustomDetails(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sensitiveDetailsJsonWo types.String
		redactedAttributes     []string
		expected               map[string]any
	}{
		"legacy details_json": {
			sensitiveDetailsJsonWo: types.StringNull(),
			expected: map[string]any{
				"host": "postgres.example.internal",
				"port": float64(5432),
			},
		},
		"write-only details override regular details": {
			sensitiveDetailsJsonWo: types.StringValue(`{"host":"secret.example.internal","password":"secret"}`),
			redactedAttributes:     []string{"host"},
			expected: map[string]any{
				"host":     "secret.example.internal",
				"port":     float64(5432),
				"password": "secret",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := DatabaseResourceModel{
				CustomDetails: testCustomDetailsValue(
					`{"host":"postgres.example.internal","port":5432}`,
					types.StringNull(),
					types.Int64Value(1),
					test.redactedAttributes...,
				),
			}
			configuredCustomDetails := testCustomDetailsValue(
				`{"host":"postgres.example.internal","port":5432}`,
				test.sensitiveDetailsJsonWo,
				types.Int64Value(1),
				test.redactedAttributes...,
			)

			engineAndDetails, diags := makeEngineAndDetailsFromModel(context.Background(), data, configuredCustomDetails)
			if diags.HasError() {
				t.Fatalf("Unexpected diagnostics: %v", diags)
			}

			actual, err := engineAndDetails.Details.AsDatabaseDetailsCustom()
			if err != nil {
				t.Fatalf("Failed to read custom database details: %v", err)
			}
			if !reflect.DeepEqual(map[string]any(actual), test.expected) {
				t.Fatalf("Unexpected custom database details: got %#v, expected %#v", actual, test.expected)
			}
		})
	}
}

func TestMakeEngineAndDetailsFromModelRejectsUnredactedOverlap(t *testing.T) {
	t.Parallel()

	data := DatabaseResourceModel{
		CustomDetails: testCustomDetailsValue(`{"host":"postgres.example.internal"}`, types.StringNull(), types.Int64Value(1)),
	}
	configuredCustomDetails := testCustomDetailsValue(
		`{"host":"postgres.example.internal"}`,
		types.StringValue(`{"host":"secret.example.internal"}`),
		types.Int64Value(1),
	)

	engineAndDetails, diags := makeEngineAndDetailsFromModel(context.Background(), data, configuredCustomDetails)
	if engineAndDetails != nil {
		t.Fatalf("Expected no database details for an unredacted overlapping attribute, got %#v", engineAndDetails)
	}
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic for an unredacted overlapping attribute")
	}
}

func TestMakeEngineAndDetailsFromModelRejectsInvalidSensitiveDetails(t *testing.T) {
	t.Parallel()

	data := DatabaseResourceModel{
		CustomDetails: testCustomDetailsValue(`{"host":"postgres.example.internal"}`, types.StringNull(), types.Int64Value(1)),
	}
	configuredCustomDetails := testCustomDetailsValue(
		`{"host":"postgres.example.internal"}`,
		types.StringValue(`[]`),
		types.Int64Value(1),
	)

	engineAndDetails, diags := makeEngineAndDetailsFromModel(context.Background(), data, configuredCustomDetails)
	if engineAndDetails != nil {
		t.Fatalf("Expected no database details for an invalid sensitive JSON object, got %#v", engineAndDetails)
	}
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic for an invalid sensitive JSON object")
	}
}

func TestMakeCustomDetailsFromResponseBodyOmitsSensitiveDetails(t *testing.T) {
	t.Parallel()

	var databaseDetails metabase.DatabaseDetails
	err := databaseDetails.FromDatabaseDetailsCustom(map[string]any{
		"host":     "secret.example.internal",
		"password": "**MetabasePass**",
	})
	if err != nil {
		t.Fatalf("Failed to prepare API database details: %v", err)
	}

	data := DatabaseResourceModel{
		CustomDetails: testCustomDetailsValue(
			`{"host":"postgres.example.internal"}`,
			types.StringNull(),
			types.Int64Value(7),
			"host",
		),
	}
	details, diags := makeCustomDetailsFromResponseBody(context.Background(), metabase.Database{
		Engine:  metabase.DatabaseEngine("postgres"),
		Details: databaseDetails,
	}, &data)
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}

	var actual CustomDetails
	diags = details.As(context.Background(), &actual, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		t.Fatalf("Failed to read Terraform custom details: %v", diags)
	}

	var storedDetails map[string]any
	if err := json.Unmarshal([]byte(actual.DetailsJson.ValueString()), &storedDetails); err != nil {
		t.Fatalf("Failed to read stored details_json: %v", err)
	}
	if _, exists := storedDetails["password"]; exists {
		t.Fatalf("Sensitive detail was retained in state details: %#v", storedDetails)
	}
	if storedDetails["host"] != "postgres.example.internal" {
		t.Fatalf("Sensitive overlapping detail replaced the configured state value: %#v", storedDetails)
	}
	if !actual.SensitiveDetailsJsonWo.IsNull() {
		t.Fatalf("Write-only details must be null in state, got %q", actual.SensitiveDetailsJsonWo.ValueString())
	}
	if actual.SensitiveDetailsJsonWoVersion.ValueInt64() != 7 {
		t.Fatalf("Write-only details version was not retained in state: got %d", actual.SensitiveDetailsJsonWoVersion.ValueInt64())
	}
}

func testAccDatabaseResource(name string, dbName string, sensitiveDetailsVersion int) string {
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
      schema-filters-type     = "inclusion"
      schema-filters-patterns = "this_schema_only"
      ssl                     = false
      tunnel-enabled          = false
      advanced-options        = false
    })

    sensitive_details_json_wo = jsonencode({
      password = "%s"
    })
    sensitive_details_json_wo_version = %d

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
		sensitiveDetailsVersion,
	)
}

func testAccCheckDatabaseDetailNotStored(resourceName string, detailName string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resourceName, "custom_details.details_json", func(value string) error {
		var details map[string]any
		if err := json.Unmarshal([]byte(value), &details); err != nil {
			return fmt.Errorf("Failed to deserialize custom database details from state: %w", err)
		}
		if _, exists := details[detailName]; exists {
			return fmt.Errorf("Database detail %q was unexpectedly stored in state.", detailName)
		}

		return nil
	})
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
				Config: providerConfig + testAccDatabaseResource("test", "🐘 PG", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseExists("metabase_database.test"),
					testAccCheckDatabaseDetailNotStored("metabase_database.test", "password"),
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "🐘 PG"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo_version", "1"),
				),
			},
			{
				ResourceName: "metabase_database.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccDatabaseResource("test", "✨ New", 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseDetailNotStored("metabase_database.test", "password"),
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "✨ New"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo_version", "2"),
				),
			},
		},
	})
}
