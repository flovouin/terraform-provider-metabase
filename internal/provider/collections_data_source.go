package provider

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &CollectionsDataSource{}

// Creates a new collections data source.
func NewCollectionsDataSource() datasource.DataSource {
	return &CollectionsDataSource{}
}

// A data source listing all the collections in Metabase.
type CollectionsDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for the collections data source.
type CollectionsDataSourceModel struct {
	Archived    types.Bool `tfsdk:"archived"`    // Whether archived collections should be returned instead of active ones.
	Collections types.List `tfsdk:"collections"` // The list of collections.
	IdsByPath   types.Map  `tfsdk:"ids_by_path"` // A map where keys are collection paths and values are collection IDs.
}

// The Terraform model for a single collection returned by the collections data source.
type CollectionsDataSourceCollectionModel struct {
	Id              types.String `tfsdk:"id"`                // The ID of the collection.
	Name            types.String `tfsdk:"name"`              // The name of the collection.
	ParentId        types.Int64  `tfsdk:"parent_id"`         // The ID of the parent collection, if any.
	Location        types.String `tfsdk:"location"`          // A path-like location, made of the IDs of the ancestors of the collection.
	Path            types.String `tfsdk:"path"`              // The human-readable path of the collection, made of the names of its ancestors and its own name.
	Description     types.String `tfsdk:"description"`       // A description for the collection.
	PersonalOwnerId types.Int64  `tfsdk:"personal_owner_id"` // The ID of the user owning this collection, if it is a personal collection.
}

// The attribute types for a single collection, used to build the list of collections.
var collectionsDataSourceCollectionAttrTypes = map[string]attr.Type{
	"id":                types.StringType,
	"name":              types.StringType,
	"parent_id":         types.Int64Type,
	"location":          types.StringType,
	"path":              types.StringType,
	"description":       types.StringType,
	"personal_owner_id": types.Int64Type,
}

func (d *CollectionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collections"
}

func (d *CollectionsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `All the Metabase collections visible to the current user.

Along with the attributes returned by the Metabase API, each collection exposes a human-readable ` + "`path`" + `, built from the names of its ancestors (e.g. ` + "`Marketing/Campaigns`" + `). This makes it possible to look up a collection by the location it has in the interface, rather than by its Metabase ID.

This data source is useful to apply the same configuration to an entire subtree of collections, for example when granting permissions to a collection and all its descendants.`,

		Attributes: map[string]schema.Attribute{
			"archived": schema.BoolAttribute{
				MarkdownDescription: "Whether archived collections should be returned instead of active ones. Defaults to `false`.",
				Optional:            true,
			},
			"collections": schema.ListNestedAttribute{
				MarkdownDescription: "The list of collections, sorted by `path`.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							MarkdownDescription: "The ID of the collection. All user-created collections have an integer ID, the automatically-created root collection's ID is `root`.",
							Computed:            true,
						},
						"name": schema.StringAttribute{
							MarkdownDescription: "The name of the collection.",
							Computed:            true,
						},
						"parent_id": schema.Int64Attribute{
							MarkdownDescription: "The ID of the parent collection. This is null for top-level collections, which belong to the root collection.",
							Computed:            true,
						},
						"location": schema.StringAttribute{
							MarkdownDescription: "A path-like location, made of the IDs of the ancestors of the collection (e.g. `/12/34/`).",
							Computed:            true,
						},
						"path": schema.StringAttribute{
							MarkdownDescription: "The human-readable path of the collection, made of the names of its ancestors and its own name, separated by `/` (e.g. `Marketing/Campaigns`). The root collection is not part of the path. An ancestor that is not returned by the API is replaced by its ID. Because collection names can themselves contain a `/`, paths are not guaranteed to be unambiguous.",
							Computed:            true,
						},
						"description": schema.StringAttribute{
							MarkdownDescription: "A description for the collection.",
							Computed:            true,
						},
						"personal_owner_id": schema.Int64Attribute{
							MarkdownDescription: "The ID of the user owning this collection, if it is a personal collection. This can be used to filter out personal collections.",
							Computed:            true,
						},
					},
				},
			},
			"ids_by_path": schema.MapAttribute{
				MarkdownDescription: "A map where keys are collection `path`s and values are the corresponding collection IDs. When several collections share the same path, only the first one is present in the map and a warning is emitted.",
				ElementType:         types.StringType,
				Computed:            true,
			},
		},
	}
}

func (d *CollectionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*metabase.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected client type when configuring Metabase data source.",
			fmt.Sprintf("Expected *metabase.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// A collection returned by the Metabase API, along with the values needed to compute its path.
type collectionWithHierarchy struct {
	collection  metabase.Collection // The collection returned by the Metabase API.
	id          string              // The ID of the collection, as a string because of the root collection.
	ancestorIds []int64             // The IDs of the ancestors of the collection, from the top-most one to the direct parent.
}

// Parses the location of a collection (e.g. `/12/34/`) and returns the IDs of its ancestors, from the top-most one to
// the direct parent. The list is empty for top-level collections.
func parseCollectionLocation(location *string) ([]int64, diag.Diagnostics) {
	var diags diag.Diagnostics

	ancestorIds := make([]int64, 0)
	if location == nil {
		return ancestorIds, diags
	}

	for _, part := range strings.Split(*location, "/") {
		// The location is usually something like `/12/34/`, which yields empty parts once split.
		if len(part) == 0 {
			continue
		}

		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			diags.AddError("Unable to parse an ancestor ID from the collection location.", *location)
			return nil, diags
		}

		ancestorIds = append(ancestorIds, id)
	}

	return ancestorIds, diags
}

// Returns the human-readable path for the given collection, using the passed map of collection names to resolve its
// ancestors.
func makeCollectionPath(c collectionWithHierarchy, namesById map[int64]string) string {
	segments := make([]string, 0, len(c.ancestorIds)+1)

	for _, ancestorId := range c.ancestorIds {
		name, ok := namesById[ancestorId]
		if !ok {
			// An ancestor can be missing from the API response, for example when it is not visible to the current user,
			// or when only archived collections are requested. Falling back to the ID keeps the path unique.
			name = strconv.FormatInt(ancestorId, 10)
		}

		segments = append(segments, name)
	}

	return strings.Join(append(segments, c.collection.Name), "/")
}

// Makes the Terraform model for a single collection, resolving its path using the passed map of collection names.
func makeCollectionsDataSourceCollectionModel(c collectionWithHierarchy, namesById map[int64]string) CollectionsDataSourceCollectionModel {
	parentId := types.Int64Null()
	if len(c.ancestorIds) > 0 {
		parentId = types.Int64Value(c.ancestorIds[len(c.ancestorIds)-1])
	}

	return CollectionsDataSourceCollectionModel{
		Id:              types.StringValue(c.id),
		Name:            types.StringValue(c.collection.Name),
		ParentId:        parentId,
		Location:        stringValueOrNull(c.collection.Location),
		Path:            types.StringValue(makeCollectionPath(c, namesById)),
		Description:     stringValueOrNull(c.collection.Description),
		PersonalOwnerId: int64ValueOrNull(c.collection.PersonalOwnerId),
	}
}

// Updates the given `CollectionsDataSourceModel` from the list of `Collection`s returned by the Metabase API.
func updateModelFromCollections(ctx context.Context, collections []metabase.Collection, data *CollectionsDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	hierarchies := make([]collectionWithHierarchy, 0, len(collections))
	namesById := make(map[int64]string, len(collections))

	for _, col := range collections {
		id, idDiags := collectionIdToString(col.Id)
		diags.Append(idDiags...)
		if diags.HasError() {
			return diags
		}

		ancestorIds, locationDiags := parseCollectionLocation(col.Location)
		diags.Append(locationDiags...)
		if diags.HasError() {
			return diags
		}

		hierarchies = append(hierarchies, collectionWithHierarchy{
			collection:  col,
			id:          *id,
			ancestorIds: ancestorIds,
		})

		// Only collections with an integer ID can be the ancestor of another collection. The root collection is not part
		// of the paths.
		if intId, err := col.Id.AsCollectionId1(); err == nil {
			namesById[int64(intId)] = col.Name
		}
	}

	models := make([]CollectionsDataSourceCollectionModel, 0, len(hierarchies))
	for _, h := range hierarchies {
		models = append(models, makeCollectionsDataSourceCollectionModel(h, namesById))
	}

	// The order in which the API returns collections is not guaranteed, and sorting avoids spurious changes in the state.
	slices.SortFunc(models, func(a CollectionsDataSourceCollectionModel, b CollectionsDataSourceCollectionModel) int {
		if c := strings.Compare(a.Path.ValueString(), b.Path.ValueString()); c != 0 {
			return c
		}

		return strings.Compare(a.Id.ValueString(), b.Id.ValueString())
	})

	idsByPath := make(map[string]string, len(models))
	for _, m := range models {
		path := m.Path.ValueString()

		if existingId, ok := idsByPath[path]; ok {
			diags.AddWarning(
				"Several collections share the same path.",
				fmt.Sprintf("Only the collection with ID %s is referenced by the path '%s' in `ids_by_path`. The collection with ID %s is not.", existingId, path, m.Id.ValueString()),
			)
			continue
		}

		idsByPath[path] = m.Id.ValueString()
	}

	collectionsList, listDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: collectionsDataSourceCollectionAttrTypes}, models)
	diags.Append(listDiags...)
	if diags.HasError() {
		return diags
	}
	data.Collections = collectionsList

	idsByPathMap, mapDiags := types.MapValueFrom(ctx, types.StringType, idsByPath)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return diags
	}
	data.IdsByPath = idsByPathMap

	return diags
}

func (d *CollectionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data CollectionsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	params := metabase.ListCollectionsParams{}
	if !data.Archived.IsNull() && !data.Archived.IsUnknown() {
		archived := data.Archived.ValueBool()
		params.Archived = &archived
	}

	listResp, err := d.client.ListCollectionsWithResponse(ctx, &params)

	resp.Diagnostics.Append(checkMetabaseResponse(listResp, err, []int{200}, "list collections")...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(updateModelFromCollections(ctx, *listResp.JSON200, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
