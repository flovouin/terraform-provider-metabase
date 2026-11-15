package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// The database detail letting the schedules be set by the user, rather than chosen by Metabase.
const userControlledSchedulingDetail = "let-user-control-scheduling"

// The content of the `metadata_sync_schedule` and `cache_field_values_schedule` attributes.
type DatabaseSchedule struct {
	ScheduleType   types.String `tfsdk:"schedule_type"`   // How often the task runs.
	ScheduleHour   types.Int64  `tfsdk:"schedule_hour"`   // The hour of the day at which the task runs.
	ScheduleMinute types.Int64  `tfsdk:"schedule_minute"` // The minute of the hour at which the task runs.
	ScheduleDay    types.String `tfsdk:"schedule_day"`    // The day of the week.
	ScheduleFrame  types.String `tfsdk:"schedule_frame"`  // The position within the month.
}

// The object type for database schedules.
var databaseScheduleObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"schedule_type":   types.StringType,
		"schedule_hour":   types.Int64Type,
		"schedule_minute": types.Int64Type,
		"schedule_day":    types.StringType,
		"schedule_frame":  types.StringType,
	},
}

var databaseScheduleDays = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

var databaseScheduleFrames = []string{"first", "mid", "last"}

// The attributes Metabase requires for a type of schedule, and the ones it ignores.
// Attributes in neither list are optional.
type databaseScheduleRules struct {
	required []string
	ignored  []string
}

var databaseScheduleRulesByType = map[metabase.DatabaseScheduleScheduleType]databaseScheduleRules{
	metabase.Hourly: {
		ignored: []string{"schedule_hour", "schedule_day", "schedule_frame"},
	},
	metabase.Daily: {
		ignored: []string{"schedule_minute", "schedule_day", "schedule_frame"},
	},
	metabase.Weekly: {
		required: []string{"schedule_day", "schedule_hour"},
		ignored:  []string{"schedule_minute", "schedule_frame"},
	},
	metabase.Monthly: {
		required: []string{"schedule_frame", "schedule_hour"},
		ignored:  []string{"schedule_minute"},
	},
}

func databaseScheduleAttribute(description string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: description + " Schedules only apply to databases with `let-user-control-scheduling` enabled, as Metabase otherwise picks its own. With `custom_details`, `let-user-control-scheduling` must be set to `true` in `details_json`, and both schedules must then be set. With `bigquery_details`, the provider enables it whenever the schedules are set, and both or neither must be set. When the attribute is removed, Metabase keeps its last schedule, which is no longer managed.",
		Optional:            true,
		Attributes: map[string]schema.Attribute{
			"schedule_type": schema.StringAttribute{
				MarkdownDescription: "How often the task runs. Can be `hourly`, `daily`, `weekly` or `monthly`.",
				Required:            true,
			},
			"schedule_hour": schema.Int64Attribute{
				MarkdownDescription: "The hour of the day (0-23) at which the task runs. Required for `weekly` and `monthly` schedules, defaults to 0 for `daily` schedules, and not allowed for `hourly` schedules.",
				Optional:            true,
			},
			"schedule_minute": schema.Int64Attribute{
				MarkdownDescription: "The minute of the hour (0-59) at which the task runs. Only allowed for `hourly` schedules, for which it defaults to 0.",
				Optional:            true,
			},
			"schedule_day": schema.StringAttribute{
				MarkdownDescription: "The day of the week (`sun`, `mon`, `tue`, `wed`, `thu`, `fri` or `sat`). Required for `weekly` schedules, and optional for `monthly` schedules with a `first` or `last` frame, which then run on the first or last such weekday of the month. Not allowed otherwise.",
				Optional:            true,
			},
			"schedule_frame": schema.StringAttribute{
				MarkdownDescription: "The position within the month. Can be `first`, `mid` (the 15th) or `last`. Required for `monthly` schedules, and not allowed otherwise.",
				Optional:            true,
			},
		},
	}
}

// Checks that a schedule is a combination of attributes which Metabase accepts and does not partly ignore.
func validateDatabaseSchedule(schedulePath path.Path, schedule DatabaseSchedule) diag.Diagnostics {
	var diags diag.Diagnostics

	invalid := func(attribute string, detail string) {
		diags.AddAttributeError(schedulePath.AtName(attribute), "Invalid database schedule", detail)
	}
	isSet := func(v attr.Value) bool {
		return !v.IsNull() && !v.IsUnknown()
	}

	if isSet(schedule.ScheduleHour) && (schedule.ScheduleHour.ValueInt64() < 0 || schedule.ScheduleHour.ValueInt64() > 23) {
		invalid("schedule_hour", "schedule_hour must be between 0 and 23.")
	}
	if isSet(schedule.ScheduleMinute) && (schedule.ScheduleMinute.ValueInt64() < 0 || schedule.ScheduleMinute.ValueInt64() > 59) {
		invalid("schedule_minute", "schedule_minute must be between 0 and 59.")
	}
	if isSet(schedule.ScheduleDay) && !slices.Contains(databaseScheduleDays, schedule.ScheduleDay.ValueString()) {
		invalid("schedule_day", fmt.Sprintf("schedule_day must be one of %s.", strings.Join(databaseScheduleDays, ", ")))
	}
	if isSet(schedule.ScheduleFrame) && !slices.Contains(databaseScheduleFrames, schedule.ScheduleFrame.ValueString()) {
		invalid("schedule_frame", fmt.Sprintf("schedule_frame must be one of %s.", strings.Join(databaseScheduleFrames, ", ")))
	}

	if schedule.ScheduleType.IsUnknown() {
		return diags
	}

	scheduleType := schedule.ScheduleType.ValueString()
	rules, ok := databaseScheduleRulesByType[metabase.DatabaseScheduleScheduleType(scheduleType)]
	if !ok {
		invalid("schedule_type", "schedule_type must be one of hourly, daily, weekly, monthly.")
		return diags
	}

	values := map[string]attr.Value{
		"schedule_hour":   schedule.ScheduleHour,
		"schedule_minute": schedule.ScheduleMinute,
		"schedule_day":    schedule.ScheduleDay,
		"schedule_frame":  schedule.ScheduleFrame,
	}
	for _, attribute := range rules.required {
		if values[attribute].IsNull() {
			invalid(attribute, fmt.Sprintf("%s is required for %s schedules.", attribute, scheduleType))
		}
	}
	for _, attribute := range rules.ignored {
		if isSet(values[attribute]) {
			invalid(attribute, fmt.Sprintf("%s is ignored by Metabase for %s schedules, and must not be set.", attribute, scheduleType))
		}
	}

	if isSet(schedule.ScheduleDay) && isSet(schedule.ScheduleFrame) && schedule.ScheduleFrame.ValueString() == "mid" {
		invalid("schedule_day", "schedule_day can only be set for monthly schedules with a first or last schedule_frame.")
	}

	return diags
}

// Checks that the schedules are consistent with the `let-user-control-scheduling` detail of custom details.
// Without it, Metabase silently replaces the schedules with its own. With it, Metabase requires both schedules when
// creating the database, so both are always required, although Metabase accepts enabling it on an existing database.
func validateCustomDetailsSchedules(customDetails CustomDetails, metadataSync types.Object, cacheFieldValues types.Object) diag.Diagnostics {
	var diags diag.Diagnostics

	if customDetails.DetailsJson.IsNull() || customDetails.DetailsJson.IsUnknown() ||
		customDetails.SensitiveDetailsJsonWo.IsUnknown() ||
		metadataSync.IsUnknown() || cacheFieldValues.IsUnknown() {
		return diags
	}

	// Details which cannot be parsed are reported when they are used.
	var details map[string]any
	if err := json.Unmarshal([]byte(customDetails.DetailsJson.ValueString()), &details); err != nil {
		return diags
	}
	userControlled, _ := details[userControlledSchedulingDetail].(bool)
	if !userControlled && !customDetails.SensitiveDetailsJsonWo.IsNull() {
		var sensitiveDetails map[string]any
		if err := json.Unmarshal([]byte(customDetails.SensitiveDetailsJsonWo.ValueString()), &sensitiveDetails); err != nil {
			return diags
		}
		userControlled, _ = sensitiveDetails[userControlledSchedulingDetail].(bool)
	}

	schedules := []struct {
		attribute string
		value     types.Object
	}{
		{"metadata_sync_schedule", metadataSync},
		{"cache_field_values_schedule", cacheFieldValues},
	}
	for _, schedule := range schedules {
		if userControlled && schedule.value.IsNull() {
			diags.AddAttributeError(
				path.Root(schedule.attribute),
				"Missing database schedule",
				fmt.Sprintf("%s must be set when %s is enabled in details_json, as Metabase requires both schedules when creating such a database.", schedule.attribute, userControlledSchedulingDetail),
			)
		}
		if !userControlled && !schedule.value.IsNull() {
			diags.AddAttributeError(
				path.Root(schedule.attribute),
				"Database schedule ignored by Metabase",
				fmt.Sprintf("Metabase replaces the schedules with its own unless %s is set to true in details_json.", userControlledSchedulingDetail),
			)
		}
	}

	return diags
}

// Checks the schedules of a BigQuery database, for which the provider enables `let-user-control-scheduling` itself.
func validateBigQuerySchedules(metadataSync types.Object, cacheFieldValues types.Object) diag.Diagnostics {
	var diags diag.Diagnostics

	if metadataSync.IsUnknown() || cacheFieldValues.IsUnknown() || metadataSync.IsNull() == cacheFieldValues.IsNull() {
		return diags
	}

	missing := "cache_field_values_schedule"
	if metadataSync.IsNull() {
		missing = "metadata_sync_schedule"
	}
	diags.AddAttributeError(
		path.Root(missing),
		"Missing database schedule",
		"metadata_sync_schedule and cache_field_values_schedule must either both be set or both be omitted, as Metabase requires both schedules once they are controlled by the user.",
	)

	return diags
}

// Returns whether any schedule is set in the model.
func hasDatabaseSchedules(data DatabaseResourceModel) bool {
	return !data.MetadataSyncSchedule.IsNull() || !data.CacheFieldValuesSchedule.IsNull()
}

// Converts a schedule attribute to its payload, or `nil` if it is not set.
func makeScheduleFromModel(ctx context.Context, value types.Object) (*metabase.DatabaseSchedule, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}

	var schedule DatabaseSchedule
	diags := value.As(ctx, &schedule, basetypes.ObjectAsOptions{})
	if diags.HasError() {
		return nil, diags
	}

	return &metabase.DatabaseSchedule{
		ScheduleType:   metabase.DatabaseScheduleScheduleType(schedule.ScheduleType.ValueString()),
		ScheduleHour:   valueInt64OrNull(schedule.ScheduleHour),
		ScheduleMinute: valueInt64OrNull(schedule.ScheduleMinute),
		ScheduleDay:    valueStringOrNull(schedule.ScheduleDay),
		ScheduleFrame:  valueStringOrNull(schedule.ScheduleFrame),
	}, diags
}

// Converts the schedule attributes of a `DatabaseResourceModel` to the `schedules` payload, or `nil` if none is set.
func makeSchedulesFromModel(ctx context.Context, data DatabaseResourceModel) (*metabase.DatabaseSchedules, diag.Diagnostics) {
	metadataSync, diags := makeScheduleFromModel(ctx, data.MetadataSyncSchedule)
	cacheFieldValues, cacheDiags := makeScheduleFromModel(ctx, data.CacheFieldValuesSchedule)
	diags.Append(cacheDiags...)
	if diags.HasError() || (metadataSync == nil && cacheFieldValues == nil) {
		return nil, diags
	}

	return &metabase.DatabaseSchedules{
		MetadataSync:     metadataSync,
		CacheFieldValues: cacheFieldValues,
	}, diags
}

// Returns the schedule as Metabase interprets it: defaults are made explicit, and ignored attributes are dropped.
func normalizeDatabaseSchedule(schedule metabase.DatabaseSchedule) metabase.DatabaseSchedule {
	orZero := func(v *int) *int {
		if v == nil {
			zero := 0
			return &zero
		}
		return v
	}

	normalized := metabase.DatabaseSchedule{ScheduleType: schedule.ScheduleType}
	switch schedule.ScheduleType {
	case metabase.Hourly:
		normalized.ScheduleMinute = orZero(schedule.ScheduleMinute)
	case metabase.Daily:
		normalized.ScheduleHour = orZero(schedule.ScheduleHour)
	case metabase.Weekly:
		normalized.ScheduleDay = schedule.ScheduleDay
		normalized.ScheduleHour = schedule.ScheduleHour
	case metabase.Monthly:
		normalized.ScheduleFrame = schedule.ScheduleFrame
		normalized.ScheduleHour = schedule.ScheduleHour
		if schedule.ScheduleFrame == nil || *schedule.ScheduleFrame != "mid" {
			normalized.ScheduleDay = schedule.ScheduleDay
		}
	}

	return normalized
}

// Makes the Terraform object for a schedule returned by the Metabase API.
// The existing value is kept when Metabase interprets it as the returned schedule.
func makeScheduleFromDatabase(ctx context.Context, remote *metabase.DatabaseSchedule, existing types.Object) (types.Object, diag.Diagnostics) {
	if remote == nil {
		return types.ObjectNull(databaseScheduleObjectType.AttrTypes), nil
	}

	normalized := normalizeDatabaseSchedule(*remote)

	current, diags := makeScheduleFromModel(ctx, existing)
	if diags.HasError() {
		return existing, diags
	}
	if current != nil && reflect.DeepEqual(normalizeDatabaseSchedule(*current), normalized) {
		return existing, diags
	}

	return types.ObjectValue(databaseScheduleObjectType.AttrTypes, map[string]attr.Value{
		"schedule_type":   types.StringValue(string(normalized.ScheduleType)),
		"schedule_hour":   int64ValueOrNull(normalized.ScheduleHour),
		"schedule_minute": int64ValueOrNull(normalized.ScheduleMinute),
		"schedule_day":    stringValueOrNull(normalized.ScheduleDay),
		"schedule_frame":  stringValueOrNull(normalized.ScheduleFrame),
	})
}

// Returns whether the schedules of a database returned by the Metabase API are controlled by the user.
func isSchedulingUserControlled(db metabase.Database) bool {
	details, err := db.Details.AsDatabaseDetailsCustom()
	if err != nil {
		return false
	}

	userControlled, _ := details[userControlledSchedulingDetail].(bool)
	return userControlled
}

// Updates the schedules of the given `DatabaseResourceModel` from the `Database` returned by the Metabase API.
// Only the schedules already in the model are updated, such that the ones which are not configured are left unmanaged.
// When importing, the schedules are read if they are controlled by the user, rather than chosen by Metabase.
func updateSchedulesFromDatabase(ctx context.Context, db metabase.Database, data *DatabaseResourceModel, imported bool) diag.Diagnostics {
	var diags diag.Diagnostics

	if imported && !isSchedulingUserControlled(db) {
		return diags
	}

	var remote metabase.DatabaseSchedules
	if db.Schedules != nil {
		remote = *db.Schedules
	}

	if imported || !data.MetadataSyncSchedule.IsNull() {
		schedule, scheduleDiags := makeScheduleFromDatabase(ctx, remote.MetadataSync, data.MetadataSyncSchedule)
		diags.Append(scheduleDiags...)
		data.MetadataSyncSchedule = schedule
	}
	if imported || !data.CacheFieldValuesSchedule.IsNull() {
		schedule, scheduleDiags := makeScheduleFromDatabase(ctx, remote.CacheFieldValues, data.CacheFieldValuesSchedule)
		diags.Append(scheduleDiags...)
		data.CacheFieldValuesSchedule = schedule
	}

	return diags
}
