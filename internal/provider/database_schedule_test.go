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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func testScheduleValue(schedule DatabaseSchedule) types.Object {
	return types.ObjectValueMust(databaseScheduleObjectType.AttrTypes, map[string]attr.Value{
		"schedule_type":   schedule.ScheduleType,
		"schedule_hour":   schedule.ScheduleHour,
		"schedule_minute": schedule.ScheduleMinute,
		"schedule_day":    schedule.ScheduleDay,
		"schedule_frame":  schedule.ScheduleFrame,
	})
}

func testHourlySchedule(minute types.Int64) types.Object {
	return testScheduleValue(DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleMinute: minute})
}

func testDailySchedule(hour types.Int64) types.Object {
	return testScheduleValue(DatabaseSchedule{ScheduleType: types.StringValue("daily"), ScheduleHour: hour})
}

var testNullSchedule = types.ObjectNull(databaseScheduleObjectType.AttrTypes)

func testDatabaseWithSchedules(t *testing.T, details string, schedules string) metabase.Database {
	var db metabase.Database
	body := fmt.Sprintf(`{"id": 1, "name": "db", "engine": "postgres", "details": %s, "schedules": %s}`, details, schedules)
	if err := json.Unmarshal([]byte(body), &db); err != nil {
		t.Fatalf("Unable to deserialize the database: %v", err)
	}
	return db
}

func TestMakeSchedulesFromModel(t *testing.T) {
	t.Parallel()

	schedules, diags := makeSchedulesFromModel(context.Background(), DatabaseResourceModel{
		MetadataSyncSchedule: testHourlySchedule(types.Int64Value(17)),
		CacheFieldValuesSchedule: testScheduleValue(DatabaseSchedule{
			ScheduleType: types.StringValue("weekly"),
			ScheduleHour: types.Int64Value(2),
			ScheduleDay:  types.StringValue("mon"),
		}),
	})
	if diags.HasError() {
		t.Fatalf("Unexpected diagnostics: %v", diags)
	}
	sync := schedules.MetadataSync
	if sync.ScheduleType != metabase.Hourly || *sync.ScheduleMinute != 17 || sync.ScheduleHour != nil {
		t.Fatalf("Unexpected metadata sync schedule: %#v", sync)
	}
	cfv := schedules.CacheFieldValues
	if cfv.ScheduleType != metabase.Weekly || *cfv.ScheduleHour != 2 || *cfv.ScheduleDay != "mon" || cfv.ScheduleFrame != nil || cfv.ScheduleMinute != nil {
		t.Fatalf("Unexpected cache field values schedule: %#v", cfv)
	}

	schedules, diags = makeSchedulesFromModel(context.Background(), DatabaseResourceModel{
		MetadataSyncSchedule:     testNullSchedule,
		CacheFieldValuesSchedule: testNullSchedule,
	})
	if diags.HasError() || schedules != nil {
		t.Fatalf("Expected no schedules when none is configured, got %#v (%v)", schedules, diags)
	}
}

func TestValidateDatabaseSchedule(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		schedule    DatabaseSchedule
		expectError bool
	}{
		"hourly": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("hourly")},
		},
		"hourly with minute": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleMinute: types.Int64Value(59)},
		},
		"hourly with out of range minute": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleMinute: types.Int64Value(60)},
			expectError: true,
		},
		"hourly with hour": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"hourly with day": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleDay: types.StringValue("mon")},
			expectError: true,
		},
		"hourly with frame": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("hourly"), ScheduleFrame: types.StringValue("first")},
			expectError: true,
		},
		"daily": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("daily")},
		},
		"daily with hour": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("daily"), ScheduleHour: types.Int64Value(23)},
		},
		"daily with out of range hour": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("daily"), ScheduleHour: types.Int64Value(24)},
			expectError: true,
		},
		"daily with minute": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("daily"), ScheduleMinute: types.Int64Value(30)},
			expectError: true,
		},
		"weekly": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleDay: types.StringValue("mon"), ScheduleHour: types.Int64Value(2)},
		},
		"weekly without hour": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleDay: types.StringValue("mon")},
			expectError: true,
		},
		"weekly without day": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"weekly with invalid day": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleDay: types.StringValue("monday"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"weekly with frame": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleDay: types.StringValue("mon"), ScheduleHour: types.Int64Value(2), ScheduleFrame: types.StringValue("first")},
			expectError: true,
		},
		"monthly": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("mid"), ScheduleHour: types.Int64Value(2)},
		},
		"monthly on a weekday": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("last"), ScheduleDay: types.StringValue("fri"), ScheduleHour: types.Int64Value(2)},
		},
		"monthly without frame": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"monthly without hour": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("first")},
			expectError: true,
		},
		"monthly with invalid frame": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("middle"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"monthly mid frame with day": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("mid"), ScheduleDay: types.StringValue("mon"), ScheduleHour: types.Int64Value(2)},
			expectError: true,
		},
		"monthly with minute": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("monthly"), ScheduleFrame: types.StringValue("first"), ScheduleHour: types.Int64Value(2), ScheduleMinute: types.Int64Value(5)},
			expectError: true,
		},
		"invalid type": {
			schedule:    DatabaseSchedule{ScheduleType: types.StringValue("yearly")},
			expectError: true,
		},
		"unknown type defers the combination rules": {
			schedule: DatabaseSchedule{ScheduleType: types.StringUnknown(), ScheduleHour: types.Int64Value(2), ScheduleMinute: types.Int64Value(5)},
		},
		"unknown values satisfy requirements": {
			schedule: DatabaseSchedule{ScheduleType: types.StringValue("weekly"), ScheduleDay: types.StringUnknown(), ScheduleHour: types.Int64Unknown()},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diags := validateDatabaseSchedule(path.Root("metadata_sync_schedule"), test.schedule)
			if diags.HasError() != test.expectError {
				t.Fatalf("Unexpected validation diagnostics: %v", diags)
			}
		})
	}
}

func TestValidateCustomDetailsSchedules(t *testing.T) {
	t.Parallel()

	daily := testDailySchedule(types.Int64Value(2))
	userControlled := types.StringValue(`{"host":"postgres.example.internal","let-user-control-scheduling":true}`)
	metabaseControlled := types.StringValue(`{"host":"postgres.example.internal"}`)

	tests := map[string]struct {
		customDetails    CustomDetails
		metadataSync     types.Object
		cacheFieldValues types.Object
		expectError      bool
	}{
		"user-controlled with both schedules": {
			customDetails:    CustomDetails{DetailsJson: userControlled},
			metadataSync:     daily,
			cacheFieldValues: daily,
		},
		"user-controlled without schedules": {
			customDetails:    CustomDetails{DetailsJson: userControlled},
			metadataSync:     testNullSchedule,
			cacheFieldValues: testNullSchedule,
			expectError:      true,
		},
		"user-controlled with a single schedule": {
			customDetails:    CustomDetails{DetailsJson: userControlled},
			metadataSync:     daily,
			cacheFieldValues: testNullSchedule,
			expectError:      true,
		},
		"user-controlled through write-only details": {
			customDetails: CustomDetails{
				DetailsJson:            metabaseControlled,
				SensitiveDetailsJsonWo: types.StringValue(`{"let-user-control-scheduling":true}`),
			},
			metadataSync:     daily,
			cacheFieldValues: daily,
		},
		"Metabase-controlled without schedules": {
			customDetails:    CustomDetails{DetailsJson: metabaseControlled},
			metadataSync:     testNullSchedule,
			cacheFieldValues: testNullSchedule,
		},
		"Metabase-controlled with schedules": {
			customDetails:    CustomDetails{DetailsJson: metabaseControlled},
			metadataSync:     daily,
			cacheFieldValues: daily,
			expectError:      true,
		},
		"explicitly Metabase-controlled with a schedule": {
			customDetails:    CustomDetails{DetailsJson: types.StringValue(`{"let-user-control-scheduling":false}`)},
			metadataSync:     daily,
			cacheFieldValues: testNullSchedule,
			expectError:      true,
		},
		"unknown details defer validation": {
			customDetails:    CustomDetails{DetailsJson: types.StringUnknown()},
			metadataSync:     daily,
			cacheFieldValues: testNullSchedule,
		},
		"unknown write-only details defer validation": {
			customDetails:    CustomDetails{DetailsJson: metabaseControlled, SensitiveDetailsJsonWo: types.StringUnknown()},
			metadataSync:     daily,
			cacheFieldValues: daily,
		},
		"unknown schedule defers validation": {
			customDetails:    CustomDetails{DetailsJson: userControlled},
			metadataSync:     types.ObjectUnknown(databaseScheduleObjectType.AttrTypes),
			cacheFieldValues: testNullSchedule,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			diags := validateCustomDetailsSchedules(test.customDetails, test.metadataSync, test.cacheFieldValues)
			if diags.HasError() != test.expectError {
				t.Fatalf("Unexpected validation diagnostics: %v", diags)
			}
		})
	}
}

func TestValidateBigQuerySchedules(t *testing.T) {
	t.Parallel()

	daily := testDailySchedule(types.Int64Value(2))

	if diags := validateBigQuerySchedules(daily, daily); diags.HasError() {
		t.Fatalf("Unexpected diagnostics with both schedules: %v", diags)
	}
	if diags := validateBigQuerySchedules(testNullSchedule, testNullSchedule); diags.HasError() {
		t.Fatalf("Unexpected diagnostics without schedules: %v", diags)
	}
	if diags := validateBigQuerySchedules(daily, testNullSchedule); !diags.HasError() {
		t.Fatal("Expected an error with only the metadata sync schedule.")
	}
	if diags := validateBigQuerySchedules(testNullSchedule, daily); !diags.HasError() {
		t.Fatal("Expected an error with only the cache field values schedule.")
	}
}

func TestMakeEngineAndDetailsFromModelBigQuerySchedules(t *testing.T) {
	t.Parallel()

	bigQueryDetails := types.ObjectValueMust(bigQueryDetailsObjectType.AttrTypes, map[string]attr.Value{
		"service_account_key":      types.StringValue("key"),
		"project_id":               types.StringNull(),
		"dataset_filters_type":     types.StringNull(),
		"dataset_filters_patterns": types.StringNull(),
	})
	daily := testDailySchedule(types.Int64Value(2))

	tests := map[string]struct {
		schedule types.Object
		expected *bool
	}{
		"with schedules":    {schedule: daily, expected: new(bool)},
		"without schedules": {schedule: testNullSchedule},
	}
	*tests["with schedules"].expected = true

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			engineAndDetails, diags := makeEngineAndDetailsFromModel(context.Background(), DatabaseResourceModel{
				BigQueryDetails:          bigQueryDetails,
				CustomDetails:            types.ObjectNull(customDetailsObjectType.AttrTypes),
				MetadataSyncSchedule:     test.schedule,
				CacheFieldValuesSchedule: test.schedule,
			}, types.ObjectNull(customDetailsObjectType.AttrTypes))
			if diags.HasError() {
				t.Fatalf("Unexpected diagnostics: %v", diags)
			}

			details, err := engineAndDetails.Details.AsDatabaseDetailsBigQuery()
			if err != nil {
				t.Fatalf("Unable to read the BigQuery details: %v", err)
			}
			if !reflect.DeepEqual(details.LetUserControlScheduling, test.expected) {
				t.Fatalf("Unexpected let-user-control-scheduling: %v", details.LetUserControlScheduling)
			}
		})
	}
}

func TestUpdateSchedulesFromDatabase(t *testing.T) {
	t.Parallel()

	const userControlled = `{"let-user-control-scheduling": true}`
	const schedules = `{
		"metadata_sync": {"schedule_type": "hourly", "schedule_hour": null, "schedule_minute": 17, "schedule_day": null, "schedule_frame": null},
		"cache_field_values": {"schedule_type": "daily", "schedule_hour": 0, "schedule_minute": 0, "schedule_day": null, "schedule_frame": null}
	}`

	tests := map[string]struct {
		details          string
		schedules        string
		imported         bool
		metadataSync     types.Object
		cacheFieldValues types.Object
		expectedSync     types.Object
		expectedCache    types.Object
	}{
		"unmanaged schedules are left out": {
			details:          userControlled,
			schedules:        schedules,
			metadataSync:     testNullSchedule,
			cacheFieldValues: testNullSchedule,
			expectedSync:     testNullSchedule,
			expectedCache:    testNullSchedule,
		},
		"equivalent schedules are kept": {
			details:          userControlled,
			schedules:        schedules,
			metadataSync:     testHourlySchedule(types.Int64Value(17)),
			cacheFieldValues: testDailySchedule(types.Int64Null()),
			expectedSync:     testHourlySchedule(types.Int64Value(17)),
			expectedCache:    testDailySchedule(types.Int64Null()),
		},
		"changed schedules are read": {
			details:          userControlled,
			schedules:        schedules,
			metadataSync:     testHourlySchedule(types.Int64Null()),
			cacheFieldValues: testDailySchedule(types.Int64Value(4)),
			expectedSync:     testHourlySchedule(types.Int64Value(17)),
			expectedCache:    testDailySchedule(types.Int64Value(0)),
		},
		"cleared schedule is removed": {
			details:          userControlled,
			schedules:        `{"metadata_sync": {"schedule_type": "hourly", "schedule_minute": 17}, "cache_field_values": null}`,
			metadataSync:     testHourlySchedule(types.Int64Value(17)),
			cacheFieldValues: testDailySchedule(types.Int64Value(4)),
			expectedSync:     testHourlySchedule(types.Int64Value(17)),
			expectedCache:    testNullSchedule,
		},
		"user-controlled schedules are imported": {
			details:          userControlled,
			schedules:        schedules,
			imported:         true,
			metadataSync:     testNullSchedule,
			cacheFieldValues: testNullSchedule,
			expectedSync:     testHourlySchedule(types.Int64Value(17)),
			expectedCache:    testDailySchedule(types.Int64Value(0)),
		},
		"Metabase-controlled schedules are not imported": {
			details:          `{}`,
			schedules:        schedules,
			imported:         true,
			metadataSync:     testNullSchedule,
			cacheFieldValues: testNullSchedule,
			expectedSync:     testNullSchedule,
			expectedCache:    testNullSchedule,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := DatabaseResourceModel{
				MetadataSyncSchedule:     test.metadataSync,
				CacheFieldValuesSchedule: test.cacheFieldValues,
			}
			diags := updateSchedulesFromDatabase(context.Background(), testDatabaseWithSchedules(t, test.details, test.schedules), &data, test.imported)
			if diags.HasError() {
				t.Fatalf("Unexpected diagnostics: %v", diags)
			}
			if !data.MetadataSyncSchedule.Equal(test.expectedSync) {
				t.Fatalf("Unexpected metadata sync schedule: %v", data.MetadataSyncSchedule)
			}
			if !data.CacheFieldValuesSchedule.Equal(test.expectedCache) {
				t.Fatalf("Unexpected cache field values schedule: %v", data.CacheFieldValuesSchedule)
			}
		})
	}
}

func TestNormalizeDatabaseSchedule(t *testing.T) {
	t.Parallel()

	hour, minute, day, mid := 3, 30, "mon", "mid"

	normalized := normalizeDatabaseSchedule(metabase.DatabaseSchedule{
		ScheduleType:   metabase.Monthly,
		ScheduleHour:   &hour,
		ScheduleMinute: &minute,
		ScheduleDay:    &day,
		ScheduleFrame:  &mid,
	})
	expected := metabase.DatabaseSchedule{ScheduleType: metabase.Monthly, ScheduleHour: &hour, ScheduleFrame: &mid}
	if !reflect.DeepEqual(normalized, expected) {
		t.Fatalf("Unexpected monthly schedule: %#v", normalized)
	}

	normalized = normalizeDatabaseSchedule(metabase.DatabaseSchedule{ScheduleType: metabase.Hourly, ScheduleHour: &hour})
	if normalized.ScheduleHour != nil || normalized.ScheduleMinute == nil || *normalized.ScheduleMinute != 0 {
		t.Fatalf("Unexpected hourly schedule: %#v", normalized)
	}
}

func testAccDatabaseResourceWithSchedules(metadataSyncSchedule string, cacheFieldValuesSchedule string) string {
	return fmt.Sprintf(`
resource "metabase_database" "test_schedules" {
  name = "🕒 PG schedules"

  custom_details = {
    engine = "postgres"

    details_json = jsonencode({
      host                        = "%s"
      port                        = 5432
      dbname                      = "%s"
      user                        = "%s"
      password                    = "%s"
      ssl                         = false
      tunnel-enabled              = false
      let-user-control-scheduling = true
    })

    redacted_attributes = [
      "password",
    ]
  }

  metadata_sync_schedule = {
    %s
  }

  cache_field_values_schedule = {
    %s
  }
}
`,
		os.Getenv("PG_HOST"),
		os.Getenv("PG_DATABASE"),
		os.Getenv("PG_USER"),
		os.Getenv("PG_PASSWORD"),
		metadataSyncSchedule,
		cacheFieldValuesSchedule,
	)
}

// Checks the schedules returned by the Metabase API, as Metabase interprets them.
func testAccCheckDatabaseSchedules(resourceName string, expected metabase.DatabaseSchedules) resource.TestCheckFunc {
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

		schedules := response.JSON200.Schedules
		if schedules == nil || schedules.MetadataSync == nil || schedules.CacheFieldValues == nil {
			return fmt.Errorf("The Metabase API did not return both schedules for the database.")
		}
		if actual := normalizeDatabaseSchedule(*schedules.MetadataSync); !reflect.DeepEqual(actual, *expected.MetadataSync) {
			return fmt.Errorf("Unexpected metadata sync schedule returned by the Metabase API: %s", response.Body)
		}
		if actual := normalizeDatabaseSchedule(*schedules.CacheFieldValues); !reflect.DeepEqual(actual, *expected.CacheFieldValues) {
			return fmt.Errorf("Unexpected cache field values schedule returned by the Metabase API: %s", response.Body)
		}

		return nil
	}
}

func TestAccDatabaseResourceSchedules(t *testing.T) {
	two, three, four, seventeen, mon := 2, 3, 4, 17, "mon"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDatabaseDestroy,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + testAccDatabaseResourceWithSchedules(
					"schedule_type   = \"hourly\"\n    schedule_minute = 17",
					"schedule_type = \"daily\"\n    schedule_hour = 2",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseExists("metabase_database.test_schedules"),
					testAccCheckDatabaseSchedules("metabase_database.test_schedules", metabase.DatabaseSchedules{
						MetadataSync:     &metabase.DatabaseSchedule{ScheduleType: metabase.Hourly, ScheduleMinute: &seventeen},
						CacheFieldValues: &metabase.DatabaseSchedule{ScheduleType: metabase.Daily, ScheduleHour: &two},
					}),
					resource.TestCheckResourceAttr("metabase_database.test_schedules", "metadata_sync_schedule.schedule_type", "hourly"),
					resource.TestCheckResourceAttr("metabase_database.test_schedules", "metadata_sync_schedule.schedule_minute", "17"),
					resource.TestCheckResourceAttr("metabase_database.test_schedules", "cache_field_values_schedule.schedule_hour", "2"),
				),
			},
			{
				ResourceName:            "metabase_database.test_schedules",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"custom_details"},
			},
			{
				Config: providerConfig + testAccDatabaseResourceWithSchedules(
					"schedule_type = \"daily\"\n    schedule_hour = 4",
					"schedule_type = \"weekly\"\n    schedule_day  = \"mon\"\n    schedule_hour = 3",
				),
				Check: resource.ComposeAggregateTestCheckFunc(
					testAccCheckDatabaseSchedules("metabase_database.test_schedules", metabase.DatabaseSchedules{
						MetadataSync:     &metabase.DatabaseSchedule{ScheduleType: metabase.Daily, ScheduleHour: &four},
						CacheFieldValues: &metabase.DatabaseSchedule{ScheduleType: metabase.Weekly, ScheduleDay: &mon, ScheduleHour: &three},
					}),
					resource.TestCheckResourceAttr("metabase_database.test_schedules", "metadata_sync_schedule.schedule_type", "daily"),
					resource.TestCheckResourceAttr("metabase_database.test_schedules", "cache_field_values_schedule.schedule_day", "mon"),
				),
			},
		},
	})
}
