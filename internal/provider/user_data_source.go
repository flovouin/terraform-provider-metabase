package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &UserDataSource{}

// Creates a new user data source.
func NewUserDataSource() datasource.DataSource {
	return &UserDataSource{}
}

// A data source obtaining details about a user.
type UserDataSource struct {
	// The Metabase API client.
	client *metabase.ClientWithResponses
}

// The Terraform model for a user data source.
type UserDataSourceModel struct {
	Id          types.Int64  `tfsdk:"id"`           // The ID of the user.
	Email       types.String `tfsdk:"email"`        // The email address of the user.
	FirstName   types.String `tfsdk:"first_name"`   // The first name of the user.
	LastName    types.String `tfsdk:"last_name"`    // The last name of the user.
	Locale      types.String `tfsdk:"locale"`       // The locale used to display the interface to the user.
	GroupIds    types.Set    `tfsdk:"group_ids"`    // The IDs of the permissions groups the user belongs to.
	IsSuperuser types.Bool   `tfsdk:"is_superuser"` // Whether the user is an administrator.
	IsActive    types.Bool   `tfsdk:"is_active"`    // Whether the user is active.
	CommonName  types.String `tfsdk:"common_name"`  // The display name for the user, computed by Metabase.
	DateJoined  types.String `tfsdk:"date_joined"`  // The date at which the user was created.
	LastLogin   types.String `tfsdk:"last_login"`   // The date at which the user last logged in.
	SsoSource   types.String `tfsdk:"sso_source"`   // The SSO provider the user authenticates with.
}

func (d *UserDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *UserDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase user.

This data source can be useful to find the Metabase ID of a user based on its email address, for example to reference a
user that was created by SSO rather than by Terraform.

Deactivated users are returned as well, with ` + "`is_active`" + ` set to ` + "`false`" + `. Reading a user never changes it: this
data source never reactivates a deactivated account.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the user. If specified, the `email` should not be specified.",
				Optional:            true,
				Computed:            true,
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The email address of the user. If specified, the `id` should not be specified.",
				Optional:            true,
				Computed:            true,
			},
			"first_name": schema.StringAttribute{
				MarkdownDescription: "The first name of the user.",
				Computed:            true,
			},
			"last_name": schema.StringAttribute{
				MarkdownDescription: "The last name of the user.",
				Computed:            true,
			},
			"locale": schema.StringAttribute{
				MarkdownDescription: "The locale used by Metabase when displaying the interface to the user.",
				Computed:            true,
			},
			"group_ids": schema.SetAttribute{
				MarkdownDescription: fmt.Sprintf(
					"The IDs of the permissions groups the user belongs to. The `All Users` group (ID `%d`) and the "+
						"`Administrators` group (ID `%d`) are managed by Metabase itself and are not reported here, see "+
						"`is_superuser` for the latter.",
					metabase.AllUsersPermissionsGroupId,
					metabase.AdministratorsPermissionsGroupId,
				),
				Computed:    true,
				ElementType: types.Int64Type,
			},
			"is_superuser": schema.BoolAttribute{
				MarkdownDescription: "Whether the user is an administrator of the Metabase instance.",
				Computed:            true,
			},
			"is_active": schema.BoolAttribute{
				MarkdownDescription: "Whether the user is active. Deactivated users cannot log in.",
				Computed:            true,
			},
			"common_name": schema.StringAttribute{
				MarkdownDescription: "The display name for the user, computed by Metabase from the first and last names.",
				Computed:            true,
			},
			"date_joined": schema.StringAttribute{
				MarkdownDescription: "The date at which the user was created.",
				Computed:            true,
			},
			"last_login": schema.StringAttribute{
				MarkdownDescription: "The date at which the user last logged in, or null if it never did.",
				Computed:            true,
			},
			"sso_source": schema.StringAttribute{
				MarkdownDescription: "The SSO provider the user authenticates with (e.g. `google`), or null for a password-based account.",
				Computed:            true,
			},
		},
	}
}

func (d *UserDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*metabase.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected client type when configuring Metabase resource.",
			fmt.Sprintf("Expected *metabase.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	d.client = client
}

// Updates the given `UserDataSourceModel` from the `User` returned by the Metabase API.
func updateDataSourceModelFromUser(ctx context.Context, u metabase.User, data *UserDataSourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	data.Id = types.Int64Value(int64(u.Id))
	data.Email = types.StringValue(u.Email)
	data.FirstName = stringValueOrNull(u.FirstName)
	data.LastName = stringValueOrNull(u.LastName)
	data.Locale = stringValueOrNull(u.Locale)
	data.CommonName = stringValueOrNull(u.CommonName)
	data.DateJoined = stringValueOrNull(u.DateJoined)
	data.LastLogin = stringValueOrNull(u.LastLogin)
	data.SsoSource = stringValueOrNull(u.SsoSource)
	data.IsSuperuser = types.BoolValue(u.IsSuperuser != nil && *u.IsSuperuser)
	data.IsActive = types.BoolValue(u.IsActive == nil || *u.IsActive)

	groupIds, ok := managedGroupIdsFromUser(u)
	if !ok {
		// The Metabase API did not return the groups of the user, which happens when the provider does not authenticate
		// as an administrator. Reporting an empty set would be misleading.
		diags.AddError(
			"The Metabase API did not return the permissions groups of the user.",
			"This usually means the credentials used by the provider do not belong to an administrator.",
		)
		return diags
	}

	groupIdsSet, groupDiags := types.SetValueFrom(ctx, types.Int64Type, groupIds)
	diags.Append(groupDiags...)
	if diags.HasError() {
		return diags
	}

	data.GroupIds = groupIdsSet

	return diags
}

// Finds a user from the list of all users in Metabase, based on its email address. Deactivated users are included in
// the search: they can legitimately be referenced, and hiding them would make it look like the email address is free.
func findUserByEmail(ctx context.Context, client *metabase.ClientWithResponses, email string) (*metabase.User, diag.Diagnostics) {
	var diags diag.Diagnostics

	status := allUsersStatus
	listResp, err := client.ListUsersWithResponse(ctx, &metabase.ListUsersParams{
		Status: &status,
		Query:  &email,
	})

	diags.Append(checkMetabaseResponse(listResp, err, []int{200}, "list users")...)
	if diags.HasError() {
		return nil, diags
	}

	// Metabase compares email addresses in a case-insensitive way, and the `query` parameter matches substrings of the
	// first name, last name and email address. The exact match is therefore performed here.
	for i := range listResp.JSON200.Data {
		if strings.EqualFold(listResp.JSON200.Data[i].Email, email) {
			return &listResp.JSON200.Data[i], diags
		}
	}

	diags.AddError("Unable to find the user given its attributes.", fmt.Sprintf("Email address: %s", email))
	return nil, diags
}

func (d *UserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data UserDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	idIsSet := !data.Id.IsNull() && !data.Id.IsUnknown()
	emailIsSet := !data.Email.IsNull() && !data.Email.IsUnknown()

	var user *metabase.User

	switch {
	case idIsSet && emailIsSet:
		resp.Diagnostics.AddError("No other attribute should be set when the user ID is defined.", "")
		return
	case idIsSet:
		id := int(data.Id.ValueInt64())

		foundUser, found, diags := readUser(ctx, d.client, id)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if !found {
			resp.Diagnostics.AddError("Unable to find the user given its attributes.", fmt.Sprintf("User ID: %d", id))
			return
		}

		user = foundUser
	case emailIsSet:
		foundUser, diags := findUserByEmail(ctx, d.client, data.Email.ValueString())
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		user = foundUser
	default:
		resp.Diagnostics.AddError("At least one attribute is required to lookup the user.", "")
		return
	}

	resp.Diagnostics.Append(updateDataSourceModelFromUser(ctx, *user, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
