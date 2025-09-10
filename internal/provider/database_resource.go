package provider

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &DatabaseResource{}

// Creates a new database resource.
func NewDatabaseResource() resource.Resource {
	return &DatabaseResource{
		MetabaseBaseResource{name: "database"},
	}
}

// A resource handling a Metabase database.
type DatabaseResource struct {
	MetabaseBaseResource
}

// The Terraform model for a database.
type DatabaseResourceModel struct {
	Id              types.Int64  `tfsdk:"id"`               // The ID of the database.
	Name            types.String `tfsdk:"name"`             // A displayable name for the database.
	BigQueryDetails types.Object `tfsdk:"bigquery_details"` // The configuration for a BigQuery database.
	CustomDetails   types.Object `tfsdk:"custom_details"`   // The configuration for a database not supported by the provider.
}

// The content of the `bigquery_details` attribute to set up a BigQuery connection.
type BigQueryDetails struct {
	ServiceAccountKey      types.String `tfsdk:"service_account_key"`      // The content of the service account key.
	ProjectId              types.String `tfsdk:"project_id"`               // The project ID to use when connecting to BigQuery.
	DatasetFiltersType     types.String `tfsdk:"dataset_filters_type"`     // The type of filter to apply when listing datasets.
	DatasetFiltersPatterns types.String `tfsdk:"dataset_filters_patterns"` // The pattern when filtering datasets.
}

// The content of the `custom_details` attribute to set up a database not supported by this provider.
type CustomDetails struct {
	Engine             types.String `tfsdk:"engine"`              // The name of the engine, as defined by Metabase.
	DetailsJson        types.String `tfsdk:"details_json"`        // A JSON string containing the details for the database.
	RedactedAttributes types.Set    `tfsdk:"redacted_attributes"` // The list of `details_json` attributes that are sent back redacted by Metabase.
}

// The object type for BigQuery details.
var bigQueryDetailsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"service_account_key":      types.StringType,
		"project_id":               types.StringType,
		"dataset_filters_type":     types.StringType,
		"dataset_filters_patterns": types.StringType,
	},
}

// The object type for custom details.
var customDetailsObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"engine":       types.StringType,
		"details_json": types.StringType,
		"redacted_attributes": types.SetType{
			ElemType: types.StringType,
		},
	},
}

func (r *DatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A database Metabase can connect to. Currently only BigQuery has a dedicated attribute, but any engine can be set up using the custom_details attribute.

The configuration of this resource requires passing sensitive credentials to the Metabase API. Those credentials will also be stored in the Terraform state. Ensure those values are not checked into a repository nor are being displayed during Terraform operations.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID for the database.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The user-displayable name for the database.",
				Required:            true,
			},
			"bigquery_details": schema.SingleNestedAttribute{
				MarkdownDescription: "Connection details when setting up a BigQuery database.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"service_account_key": schema.StringAttribute{
						MarkdownDescription: "The content of the service account key file.",
						Required:            true,
						Sensitive:           true,
					},
					"project_id": schema.StringAttribute{
						MarkdownDescription: "The ID of the GCP project containing the BigQuery datasets.",
						Optional:            true,
					},
					"dataset_filters_type": schema.StringAttribute{
						MarkdownDescription: "The behavior of how BigQuery datasets should be selected. Can be `inclusion`, `exclusion`, or `all`.",
						Optional:            true,
					},
					"dataset_filters_patterns": schema.StringAttribute{
						MarkdownDescription: "The pattern used by the `dataset-filters-type`.",
						Optional:            true,
					},
				},
			},
			"custom_details": schema.SingleNestedAttribute{
				MarkdownDescription: "Connection details when setting up a database which is not supported by this provider.",
				Optional:            true,
				Attributes: map[string]schema.Attribute{
					"engine": schema.StringAttribute{
						MarkdownDescription: "The name of the engine, as defined by Metabase.",
						Required:            true,
					},
					"details_json": schema.StringAttribute{
						MarkdownDescription: "The details for the database, as a JSON string. `jsonencode` can be used for clarity.",
						Required:            true,
					},
					"redacted_attributes": schema.SetAttribute{
						ElementType:         types.StringType,
						MarkdownDescription: "The list of `details_json` attributes that are sent back redacted by Metabase.",
						Optional:            true,
					},
				},
			},
		},
	}
}

// Makes the Terraform object for the `bigquery_details` field.
func makeBigQueryDetailsFromDatabase(ctx context.Context, db metabase.Database, data *DatabaseResourceModel) (*basetypes.ObjectValue, diag.Diagnostics) {
	var diags diag.Diagnostics

	ddbq, err := db.Details.AsDatabaseDetailsBigQuery()
	if err != nil {
		diags.AddError("Unable to parse database details for BigQuery engine.", err.Error())
		return nil, diags
	}

	// Metabase returns a redacted value for this field. However it can still be useful to use it as default when the
	// resource is imported.
	serviceAccountKey := ddbq.ServiceAccountJson

	// If available, retrieve the existing database configuration to use it instead of the redacted value returned by
	// the Metabase API.
	if !data.BigQueryDetails.IsNull() {
		var bqd BigQueryDetails
		diags.Append(data.BigQueryDetails.As(ctx, &bqd, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		serviceAccountKey = bqd.ServiceAccountKey.ValueString()
	}

	details, objectDiags := types.ObjectValue(bigQueryDetailsObjectType.AttrTypes, map[string]attr.Value{
		"service_account_key":      types.StringValue(serviceAccountKey),
		"project_id":               stringValueOrNull(ddbq.ProjectId),
		"dataset_filters_type":     stringValueOrNull(ddbq.DatasetFiltersType),
		"dataset_filters_patterns": stringValueOrNull(ddbq.DatasetFiltersPatterns),
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &details, diags
}

// Makes the Terraform object for the `custom_details` field.
func makeCustomDetailsFromResponseBody(ctx context.Context, db metabase.Database, data *DatabaseResourceModel) (*basetypes.ObjectValue, diag.Diagnostics) {
	var diags diag.Diagnostics

	engine := string(db.Engine)

	rawDetails, err := db.Details.AsDatabaseDetailsCustom()
	if err != nil {
		diags.AddError("Failed to cast details attribute to object in database response.", err.Error())
		return nil, diags
	}

	var detailsJson string
	var existingDetails map[string]any
	redactedAttributesValue := types.SetNull(types.StringType)
	if !data.CustomDetails.IsNull() {
		var cd CustomDetails
		diags.Append(data.CustomDetails.As(ctx, &cd, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		redactedAttributesValue = cd.RedactedAttributes
		var redactedAttributes []string
		if !cd.RedactedAttributes.IsNull() {
			diags.Append(cd.RedactedAttributes.ElementsAs(ctx, &redactedAttributes, false)...)
			if diags.HasError() {
				return nil, diags
			}
		}

		if !cd.DetailsJson.IsNull() {
			detailsJson = cd.DetailsJson.ValueString()
			err := json.Unmarshal([]byte(detailsJson), &existingDetails)
			if err != nil {
				diags.AddError("Error deserializing existing custom details JSON.", err.Error())
				return nil, diags
			}

			// Replacing redacted fields from the Metabase response by values found in the state (that aren't redacted).
			// This ensures redacted values don't mess up with the equality test, and that they are saved back into the state.
			for _, attribute := range redactedAttributes {
				value, ok := existingDetails[attribute]
				if !ok {
					continue
				}

				rawDetails[attribute] = value
			}
		}
	}

	// Removing attributes that do not exist in Terraform to avoid unexpected changes of the JSON string.
	// Metabase might add optional attributes after the creation of the database and this should not change the state.
	for attribute := range rawDetails {
		_, exists := existingDetails[attribute]
		if !exists {
			delete(rawDetails, attribute)
		}
	}

	if existingDetails == nil || !reflect.DeepEqual(existingDetails, rawDetails) {
		detailsBytes, err := json.Marshal(rawDetails)
		if err != nil {
			diags.AddError("Error serializing new JSON value for database details.", err.Error())
			return nil, diags
		}

		detailsJson = string(detailsBytes)
	}

	details, objectDiags := types.ObjectValue(customDetailsObjectType.AttrTypes, map[string]attr.Value{
		"engine":              types.StringValue(engine),
		"details_json":        types.StringValue(detailsJson),
		"redacted_attributes": redactedAttributesValue,
	})
	diags.Append(objectDiags...)
	if diags.HasError() {
		return nil, diags
	}

	return &details, diags
}

// Updates the given `DatabaseResourceModel` from the `Database` returned by the Metabase API.
func updateModelFromDatabase(ctx context.Context, db metabase.Database, data *DatabaseResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(db.Id))
	data.Name = types.StringValue(db.Name)

	switch db.Engine {
	case metabase.BigqueryCloudSdk:
		details, bqDiags := makeBigQueryDetailsFromDatabase(ctx, db, data)
		diags.Append(bqDiags...)
		if diags.HasError() {
			return diags
		}

		data.BigQueryDetails = *details
		data.CustomDetails = types.ObjectNull(customDetailsObjectType.AttrTypes)
	default:
		details, customDiags := makeCustomDetailsFromResponseBody(ctx, db, data)
		diags.Append(customDiags...)
		if diags.HasError() {
			return diags
		}

		data.BigQueryDetails = types.ObjectNull(bigQueryDetailsObjectType.AttrTypes)
		data.CustomDetails = *details
	}

	return diags
}

// Contains the two fields fully describing the connection to a database.
// This can then be used to populate payloads when making requests against the database API.
type DatabaseEngineAndDetails struct {
	// The name of the engine for the connection.
	Engine metabase.DatabaseEngine
	// The details for the connection.
	Details metabase.DatabaseDetails
}

// Converts a `DatabaseResourceModel` to a `DatabaseEngineAndDetails` that can be used to make requests against the database API.
func makeEngineAndDetailsFromModel(ctx context.Context, data DatabaseResourceModel) (*DatabaseEngineAndDetails, diag.Diagnostics) {
	var diags diag.Diagnostics

	var engine metabase.DatabaseEngine
	var details metabase.DatabaseDetails

	if !data.BigQueryDetails.IsNull() {
		var bqd BigQueryDetails
		diags.Append(data.BigQueryDetails.As(ctx, &bqd, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		engine = metabase.BigqueryCloudSdk

		err := details.FromDatabaseDetailsBigQuery(metabase.DatabaseDetailsBigQuery{
			ServiceAccountJson:     bqd.ServiceAccountKey.ValueString(),
			ProjectId:              valueStringOrNull(bqd.ProjectId),
			DatasetFiltersType:     valueApproximateStringOrNull[metabase.DatabaseDetailsBigQueryDatasetFiltersType](bqd.DatasetFiltersType),
			DatasetFiltersPatterns: valueStringOrNull(bqd.DatasetFiltersPatterns),
		})
		if err != nil {
			diags.AddError("Failed to prepare database payload from Terraform model.", err.Error())
			return nil, diags
		}
	} else if !data.CustomDetails.IsNull() {
		var cd CustomDetails
		diags.Append(data.CustomDetails.As(ctx, &cd, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		engine = metabase.DatabaseEngine(cd.Engine.ValueString())

		var rawDetails map[string]any
		err := json.Unmarshal([]byte(cd.DetailsJson.ValueString()), &rawDetails)
		if err != nil {
			diags.AddError("Unable to deserialize details_json as an object.", err.Error())
			return nil, diags
		}

		err = details.FromDatabaseDetailsCustom(rawDetails)
		if err != nil {
			diags.AddError("Failed to prepare database payload from Terraform model.", err.Error())
			return nil, diags
		}
	} else {
		diags.AddError(
			"Could not create database details from Terraform model.",
			"Failed to find valid details for an engine.",
		)
		return nil, diags
	}

	return &DatabaseEngineAndDetails{
		Engine:  engine,
		Details: details,
	}, diags
}

func (r *DatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	engineAndDetails, diags := makeEngineAndDetailsFromModel(ctx, *data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreateDatabaseWithResponse(ctx, metabase.CreateDatabaseBody{
		Name:    data.Name.ValueString(),
		Engine:  engineAndDetails.Engine,
		Details: engineAndDetails.Details,
	})

	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create database")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromDatabase(ctx, *createResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *DatabaseResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	getResp, err := r.client.GetDatabaseWithResponse(ctx, int(data.Id.ValueInt64()))

	resp.Diagnostics.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get database")...)
	if resp.Diagnostics.HasError() {
		return
	}

	if getResp.StatusCode() == 404 {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(updateModelFromDatabase(ctx, *getResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data *DatabaseResourceModel
	var state *DatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := metabase.UpdateDatabaseBody{
		Name: valueStringOrNull(data.Name),
	}

	// Only updating database details if they have changed. This avoids unnecessarily passing credentials in API calls.
	if !state.BigQueryDetails.Equal(data.BigQueryDetails) ||
		!state.CustomDetails.Equal(data.CustomDetails) {
		engineAndDetails, diags := makeEngineAndDetailsFromModel(ctx, *data)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		body.Engine = &engineAndDetails.Engine
		body.Details = &engineAndDetails.Details
	}

	updateResp, err := r.client.UpdateDatabaseWithResponse(ctx, int(data.Id.ValueInt64()), body)

	resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update database")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromDatabase(ctx, *updateResp.JSON200, data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *DatabaseResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteResp, err := r.client.DeleteDatabaseWithResponse(ctx, int(data.Id.ValueInt64()))

	resp.Diagnostics.Append(checkMetabaseResponse(deleteResp, err, []int{204}, "delete database")...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *DatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
