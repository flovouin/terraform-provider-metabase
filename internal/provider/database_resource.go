package provider

import (
	"context"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithSchema = &DatabaseResource{}
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
}

// The content of the `bigquery_details` attribute to set up a BigQuery connection.
type BigQueryDetails struct {
	ServiceAccountKey      types.String `tfsdk:"service_account_key"`      // The content of the service account key.
	ProjectId              types.String `tfsdk:"project_id"`               // The project ID to use when connecting to BigQuery.
	DatasetFiltersType     types.String `tfsdk:"dataset_filters_type"`     // The type of filter to apply when listing datasets.
	DatasetFiltersPatterns types.String `tfsdk:"dataset_filters_patterns"` // The pattern when filtering datasets.
}

func (r *DatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A database Metabase can connect to. Currently only BigQuery is supported. Open [an issue](https://github.com/flovouin/terraform-provider-metabase/issues) to request support for other database engines.

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
			// Once several database engines are supported this won't be required anymore, but exactly one engine will have to
			// be defined.
			"bigquery_details": schema.SingleNestedAttribute{
				MarkdownDescription: "Connection details when setting up a BigQuery database.",
				Required:            true,
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
		},
	}
}

// Updates the given `DatabaseResourceModel` from the `Database` returned by the Metabase API.
func updateModelFromDatabase(ctx context.Context, db metabase.Database, data *DatabaseResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(db.Id))
	data.Name = types.StringValue(db.Name)

	switch db.Engine {
	case metabase.BigqueryCloudSdk:
		// Metabase returns a redacted value for this field. However it can still be useful to use it as default when the
		// resource is imported.
		serviceAccountKey := db.Details.ServiceAccountJson

		// If available, retrieve the existing database configuration to use it instead of the redacted value returned by
		// the Metabase API.
		if !data.BigQueryDetails.IsNull() {
			var bqd BigQueryDetails
			diags.Append(data.BigQueryDetails.As(ctx, &bqd, types.ObjectAsOptions{})...)
			if diags.HasError() {
				return diags
			}

			serviceAccountKey = bqd.ServiceAccountKey.ValueString()
		}

		details, objectDiags := types.ObjectValue(map[string]attr.Type{
			"service_account_key":      types.StringType,
			"project_id":               types.StringType,
			"dataset_filters_type":     types.StringType,
			"dataset_filters_patterns": types.StringType,
		}, map[string]attr.Value{
			"service_account_key":      types.StringValue(serviceAccountKey),
			"project_id":               stringValueOrNull(db.Details.ProjectId),
			"dataset_filters_type":     stringValueOrNull(db.Details.DatasetFiltersType),
			"dataset_filters_patterns": stringValueOrNull(db.Details.DatasetFiltersPatterns),
		})
		diags.Append(objectDiags...)
		if diags.HasError() {
			return diags
		}

		// When other engines are introduced, they should all be set to null except the one that is defined.
		data.BigQueryDetails = details
	default:
		diags.AddError("Unable to parse unsupported engine type.", string(db.Engine))
		return diags
	}

	return diags
}

// Contains the two fields fully describing the connection to a database.
// This can then be used to populate payloads when making requests against the database API.
type DatabaseEngineAndDetails struct {
	// The name of the engine for the connection.
	Engine metabase.DatabaseEngine
	// If the connection is to a BigQuery database, the details for it. `nil` otherwise.
	BigQueryDetails *metabase.DatabaseDetailsBigQuery
}

// Converts a `DatabaseResourceModel` to a `DatabaseEngineAndDetails` that can be used to make requests against the database API.
func makeEngineAndDetailsFromModel(ctx context.Context, data DatabaseResourceModel) (*DatabaseEngineAndDetails, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !data.BigQueryDetails.IsNull() {
		var bqd BigQueryDetails
		diags.Append(data.BigQueryDetails.As(ctx, &bqd, types.ObjectAsOptions{})...)
		if diags.HasError() {
			return nil, diags
		}

		return &DatabaseEngineAndDetails{
			Engine: metabase.BigqueryCloudSdk,
			BigQueryDetails: &metabase.DatabaseDetailsBigQuery{
				ServiceAccountJson:     bqd.ServiceAccountKey.ValueString(),
				ProjectId:              valueStringOrNull(bqd.ProjectId),
				DatasetFiltersType:     valueApproximateStringOrNull[metabase.DatabaseDetailsBigQueryDatasetFiltersType](bqd.DatasetFiltersType),
				DatasetFiltersPatterns: valueStringOrNull(bqd.DatasetFiltersPatterns),
			},
		}, diags
	}

	diags.AddError(
		"Could not create database details from Terraform model.",
		"Failed to find valid details for an engine.",
	)
	return nil, diags
}

func (r *DatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *DatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	details, diags := makeEngineAndDetailsFromModel(ctx, *data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreateDatabaseWithResponse(ctx, metabase.CreateDatabaseBody{
		Name:    data.Name.ValueString(),
		Engine:  details.Engine,
		Details: *details.BigQueryDetails,
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
	if !state.BigQueryDetails.Equal(data.BigQueryDetails) {
		details, diags := makeEngineAndDetailsFromModel(ctx, *data)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		body.Engine = &details.Engine
		body.Details = details.BigQueryDetails
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
