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
		expected               map[string]any
	}{
		"legacy details_json": {
			sensitiveDetailsJsonWo: types.StringNull(),
			expected: map[string]any{
				"host": "postgres.example.internal",
				"port": float64(5432),
			},
		},
		"write-only details are merged with regular details": {
			sensitiveDetailsJsonWo: types.StringValue(`{"password":"secret"}`),
			expected: map[string]any{
				"host":     "postgres.example.internal",
				"port":     float64(5432),
				"password": "secret",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			sensitiveDetailsVersion := types.Int64Null()
			if !test.sensitiveDetailsJsonWo.IsNull() {
				sensitiveDetailsVersion = types.Int64Value(1)
			}

			data := DatabaseResourceModel{
				CustomDetails: testCustomDetailsValue(
					`{"host":"postgres.example.internal","port":5432}`,
					types.StringNull(),
					sensitiveDetailsVersion,
				),
			}
			configuredCustomDetails := testCustomDetailsValue(
				`{"host":"postgres.example.internal","port":5432}`,
				test.sensitiveDetailsJsonWo,
				sensitiveDetailsVersion,
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

func TestMakeEngineAndDetailsFromModelRejectsOverlap(t *testing.T) {
	t.Parallel()

	data := DatabaseResourceModel{
		CustomDetails: testCustomDetailsValue(`{"host":"postgres.example.internal"}`, types.StringNull(), types.Int64Value(1), "host"),
	}
	configuredCustomDetails := testCustomDetailsValue(
		`{"host":"postgres.example.internal"}`,
		types.StringValue(`{"host":"secret.example.internal"}`),
		types.Int64Value(1),
		"host",
	)

	engineAndDetails, diags := makeEngineAndDetailsFromModel(context.Background(), data, configuredCustomDetails)
	if engineAndDetails != nil {
		t.Fatalf("Expected no database details for an overlapping attribute, got %#v", engineAndDetails)
	}
	if !diags.HasError() {
		t.Fatal("Expected an error diagnostic for an overlapping attribute")
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
		"host":     "postgres.example.internal",
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
	if !actual.SensitiveDetailsJsonWo.IsNull() {
		t.Fatalf("Write-only details must be null in state, got %q", actual.SensitiveDetailsJsonWo.ValueString())
	}
	if actual.SensitiveDetailsJsonWoVersion.ValueInt64() != 7 {
		t.Fatalf("Write-only details version was not retained in state: got %d", actual.SensitiveDetailsJsonWoVersion.ValueInt64())
	}
}

func TestMakeCustomDetailsFromResponseBodyPreservesRedactedDetails(t *testing.T) {
	t.Parallel()

	var databaseDetails metabase.DatabaseDetails
	err := databaseDetails.FromDatabaseDetailsCustom(map[string]any{
		"host":     "postgres.example.internal",
		"password": "**MetabasePass**",
	})
	if err != nil {
		t.Fatalf("Failed to prepare API database details: %v", err)
	}

	data := DatabaseResourceModel{
		CustomDetails: testCustomDetailsValue(
			`{"host":"postgres.example.internal","password":"secret"}`,
			types.StringNull(),
			types.Int64Null(),
			"password",
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
	if storedDetails["password"] != "secret" {
		t.Fatalf("Redacted detail was not preserved in state: %#v", storedDetails)
	}
}

func TestValidateCustomDetails(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		customDetails CustomDetails
		expectError   bool
	}{
		"legacy details": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringNull(),
				SensitiveDetailsJsonWoVersion: types.Int64Null(),
			},
		},
		"write-only details and version": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringValue(`{"password":"secret"}`),
				SensitiveDetailsJsonWoVersion: types.Int64Value(1),
			},
		},
		"unknown details defer overlap validation": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringUnknown(),
				SensitiveDetailsJsonWo:        types.StringValue(`{"password":"secret"}`),
				SensitiveDetailsJsonWoVersion: types.Int64Value(1),
			},
		},
		"unknown write-only details defer overlap validation": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringUnknown(),
				SensitiveDetailsJsonWoVersion: types.Int64Value(1),
			},
		},
		"write-only details without version": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringValue(`{"password":"secret"}`),
				SensitiveDetailsJsonWoVersion: types.Int64Null(),
			},
			expectError: true,
		},
		"version without write-only details": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringNull(),
				SensitiveDetailsJsonWoVersion: types.Int64Value(1),
			},
			expectError: true,
		},
		"overlapping details": {
			customDetails: CustomDetails{
				DetailsJson:                   types.StringValue(`{"host":"postgres.example.internal"}`),
				SensitiveDetailsJsonWo:        types.StringValue(`{"host":"secret.example.internal"}`),
				SensitiveDetailsJsonWoVersion: types.Int64Value(1),
				RedactedAttributes:            types.SetValueMust(types.StringType, []attr.Value{types.StringValue("host")}),
			},
			expectError: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diags := validateCustomDetails(test.customDetails)
			if diags.HasError() != test.expectError {
				t.Fatalf("Unexpected validation diagnostics: %v", diags)
			}
		})
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

resource "metabase_database" "%s_write_only" {
  name = "%s write-only"

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
  }
}
`,
		name,
		dbName,
		os.Getenv("PG_HOST"),
		os.Getenv("PG_DATABASE"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
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

func testAccCheckDatabaseDetailStored(resourceName string, detailName string, expectedValue string) resource.TestCheckFunc {
	return resource.TestCheckResourceAttrWith(resourceName, "custom_details.details_json", func(value string) error {
		var details map[string]any
		if err := json.Unmarshal([]byte(value), &details); err != nil {
			return fmt.Errorf("Failed to deserialize custom database details from state: %w", err)
		}
		if detailValue, exists := details[detailName]; !exists {
			return fmt.Errorf("Database detail %q was unexpectedly missing from state.", detailName)
		} else if detailValue != expectedValue {
			return fmt.Errorf("Database detail %q did not retain its configured value: got %#v, expected %#v.", detailName, detailValue, expectedValue)
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
					testAccCheckDatabaseDetailStored("metabase_database.test", "password", os.Getenv("PG_PASSWORD")),
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "🐘 PG"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo_version"),
					testAccCheckDatabaseExists("metabase_database.test_write_only"),
					testAccCheckDatabaseDetailNotStored("metabase_database.test_write_only", "password"),
					resource.TestCheckResourceAttrSet("metabase_database.test_write_only", "id"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "name", "🐘 PG write-only"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "custom_details.sensitive_details_json_wo_version", "1"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "custom_details.redacted_attributes"),
				),
			},
			{
				ResourceName: "metabase_database.test",
				ImportState:  true,
			},
			{
				Config: providerConfig + testAccDatabaseResource("test", "✨ New", 2),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseDetailStored("metabase_database.test", "password", os.Getenv("PG_PASSWORD")),
					resource.TestCheckResourceAttrSet("metabase_database.test", "id"),
					resource.TestCheckResourceAttr("metabase_database.test", "name", "✨ New"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckNoResourceAttr("metabase_database.test", "custom_details.sensitive_details_json_wo_version"),
					testAccCheckDatabaseDetailNotStored("metabase_database.test_write_only", "password"),
					resource.TestCheckResourceAttrSet("metabase_database.test_write_only", "id"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "name", "✨ New write-only"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "bigquery_details"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "custom_details.engine", "postgres"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "custom_details.sensitive_details_json_wo"),
					resource.TestCheckResourceAttr("metabase_database.test_write_only", "custom_details.sensitive_details_json_wo_version", "2"),
					resource.TestCheckNoResourceAttr("metabase_database.test_write_only", "custom_details.redacted_attributes"),
				),
			},
		},
	})
}
