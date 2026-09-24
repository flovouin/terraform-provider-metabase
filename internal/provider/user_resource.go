package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ resource.ResourceWithImportState = &UserResource{}
var _ resource.ResourceWithValidateConfig = &UserResource{}

// The message returned by the Metabase API when creating a user with an email address that is already taken. Because
// Metabase never deletes users, this also happens when the existing user has been deactivated.
const emailAlreadyInUseError = "Email address already in use."

// The value for the `status` parameter of the endpoint listing users, such that deactivated users are returned as well.
const allUsersStatus = "all"

// Creates a new user resource.
func NewUserResource() resource.Resource {
	return &UserResource{
		MetabaseBaseResource{name: "user"},
	}
}

// A resource handling a Metabase user.
type UserResource struct {
	MetabaseBaseResource
}

// The Terraform model for a user.
type UserResourceModel struct {
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

func (r *UserResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `A Metabase user.

Metabase never permanently deletes users: ` + "`DELETE /api/user/{id}`" + ` only deactivates them, which is what this
resource does when it is destroyed. A deactivated user keeps its ID and its email address, and can be reactivated.

**This resource never reactivates a user implicitly.** Reading a deactivated user reports ` + "`is_active = false`" + ` in the
state rather than restoring the account, so a user deactivated outside of Terraform (an employee who left, for
instance) stays deactivated across refreshes, plans and imports. Reactivating is an explicit change: set
` + "`is_active = true`" + ` in the configuration, review the resulting plan, and apply it.

Because the email address of a deactivated user remains in use, creating a new ` + "`metabase_user`" + ` with the email address
of a deactivated one fails. Import the existing user instead, and set ` + "`is_active = true`" + ` if it should be restored.`,

		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				MarkdownDescription: "The ID of the user.",
				Computed:            true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The email address of the user, which is also used as the login.",
				Required:            true,
			},
			"first_name": schema.StringAttribute{
				MarkdownDescription: "The first name of the user. Metabase does not allow changing it for users authenticating through SSO. " +
					"Removing the attribute from the configuration leaves the value untouched in Metabase, which does not support clearing it.",
				Optional: true,
				Computed: true,
			},
			"last_name": schema.StringAttribute{
				MarkdownDescription: "The last name of the user. Metabase does not allow changing it for users authenticating through SSO. " +
					"Removing the attribute from the configuration leaves the value untouched in Metabase, which does not support clearing it.",
				Optional: true,
				Computed: true,
			},
			"locale": schema.StringAttribute{
				MarkdownDescription: "The locale used by Metabase when displaying the interface to the user, e.g. `en` or `fr`. " +
					"When unset, the instance default is used. Removing the attribute from the configuration leaves the value " +
					"untouched in Metabase, which does not support clearing it.",
				Optional: true,
				Computed: true,
			},
			"group_ids": schema.SetAttribute{
				MarkdownDescription: fmt.Sprintf(
					"The IDs of the permissions groups the user belongs to. This is authoritative: groups that are not "+
						"listed are removed from the user. Two groups are managed by Metabase itself and must not be "+
						"listed here: the `All Users` group (ID `%d`), which contains every user and cannot be left, and "+
						"the `Administrators` group (ID `%d`), which Metabase keeps in sync with `is_superuser` and which "+
						"is therefore managed through that attribute instead.",
					metabase.AllUsersPermissionsGroupId,
					metabase.AdministratorsPermissionsGroupId,
				),
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
			},
			"is_superuser": schema.BoolAttribute{
				MarkdownDescription: "Whether the user is an administrator of the Metabase instance. Administrators are members of the `Administrators` permissions group.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"is_active": schema.BoolAttribute{
				MarkdownDescription: "Whether the user is active. Deactivated users cannot log in. Leave this unset to " +
					"let Metabase own the value: the provider then reports deactivations but never undoes them. Set it " +
					"to `true` to explicitly (re)activate a user, or to `false` to deactivate one.",
				Optional: true,
				Computed: true,
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

func (r *UserResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data *UserResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data == nil {
		return
	}

	if data.GroupIds.IsNull() || data.GroupIds.IsUnknown() {
		return
	}

	var groupIds []types.Int64
	resp.Diagnostics.Append(data.GroupIds.ElementsAs(ctx, &groupIds, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	for _, g := range groupIds {
		if g.IsNull() || g.IsUnknown() {
			continue
		}

		switch g.ValueInt64() {
		case metabase.AllUsersPermissionsGroupId:
			resp.Diagnostics.AddAttributeError(
				path.Root("group_ids"),
				"The `All Users` permissions group cannot be managed through `group_ids`.",
				fmt.Sprintf("Metabase adds every user to the `All Users` group (ID %d) and does not allow removing them "+
					"from it. Remove the ID from `group_ids`, the membership is implicit.", metabase.AllUsersPermissionsGroupId),
			)
		case metabase.AdministratorsPermissionsGroupId:
			resp.Diagnostics.AddAttributeError(
				path.Root("group_ids"),
				"The `Administrators` permissions group cannot be managed through `group_ids`.",
				fmt.Sprintf("Metabase keeps membership of the `Administrators` group (ID %d) in sync with the "+
					"`is_superuser` flag. Remove the ID from `group_ids` and set `is_superuser` to `true` instead.",
					metabase.AdministratorsPermissionsGroupId),
			)
		}
	}
}

// Returns the IDs of the permissions groups the given user belongs to, along with whether the Metabase API actually
// returned them. The endpoints operating on a single user return `user_group_memberships`, while the endpoint listing
// users returns `group_ids`. Groups managed by Metabase itself are filtered out, see the `group_ids` attribute.
func managedGroupIdsFromUser(u metabase.User) ([]int64, bool) {
	var apiGroupIds []int

	if u.UserGroupMemberships != nil {
		for _, m := range *u.UserGroupMemberships {
			apiGroupIds = append(apiGroupIds, m.Id)
		}
	} else if u.GroupIds != nil {
		apiGroupIds = append(apiGroupIds, *u.GroupIds...)
	} else {
		// Neither representation was returned, e.g. because the Metabase credentials do not belong to an administrator.
		// The caller should keep whatever it already knows rather than assuming the user belongs to no group at all.
		return nil, false
	}

	groupIds := make([]int64, 0, len(apiGroupIds))
	for _, id := range apiGroupIds {
		if id == metabase.AllUsersPermissionsGroupId || id == metabase.AdministratorsPermissionsGroupId {
			continue
		}

		groupIds = append(groupIds, int64(id))
	}

	return groupIds, true
}

// Returns the value of a Terraform `String` type, or `nil` if it is null or still unknown. Attributes that are both
// optional and computed are unknown in the plan when they are not set in the configuration, and Metabase should then
// keep whatever value it already holds rather than receive an empty one.
func valueStringOrNullIfUnknown(v types.String) *string {
	if v.IsUnknown() {
		return nil
	}

	return valueStringOrNull(v)
}

// Returns the group memberships to send to the Metabase API for a user, from the groups referenced in the Terraform
// resource. The `All Users` group is always added because Metabase rejects its removal, and the `Administrators` group
// is added when the user should be a superuser, because Metabase requires both to agree.
func makeUserGroupMemberships(ctx context.Context, groupIds types.Set, isSuperuser types.Bool) ([]metabase.UserGroupMembership, diag.Diagnostics) {
	var diags diag.Diagnostics

	apiGroupIds := []int{metabase.AllUsersPermissionsGroupId}

	if isSuperuser.ValueBool() {
		apiGroupIds = append(apiGroupIds, metabase.AdministratorsPermissionsGroupId)
	}

	if !groupIds.IsNull() && !groupIds.IsUnknown() {
		var ids []int64
		diags.Append(groupIds.ElementsAs(ctx, &ids, false)...)
		if diags.HasError() {
			return nil, diags
		}

		for _, id := range ids {
			apiGroupIds = append(apiGroupIds, int(id))
		}
	}

	memberships := make([]metabase.UserGroupMembership, 0, len(apiGroupIds))
	for _, id := range apiGroupIds {
		memberships = append(memberships, metabase.UserGroupMembership{Id: id})
	}

	return memberships, diags
}

// Updates the given `UserResourceModel` from the `User` returned by the Metabase API.
func updateModelFromUser(ctx context.Context, u metabase.User, data *UserResourceModel) diag.Diagnostics {
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
	// Metabase only omits `is_active` from responses that do not describe the user as an administrator would see it.
	// Assuming the user is active in that case matches what the rest of the API exposes.
	data.IsActive = types.BoolValue(u.IsActive == nil || *u.IsActive)

	if groupIds, ok := managedGroupIdsFromUser(u); ok {
		groupIdsSet, groupDiags := types.SetValueFrom(ctx, types.Int64Type, groupIds)
		diags.Append(groupDiags...)
		if diags.HasError() {
			return diags
		}

		data.GroupIds = groupIdsSet
	}

	return diags
}

// Reads a single user from the Metabase API, including deactivated ones. The second return value is whether the user
// exists at all.
//
// Recent Metabase versions return deactivated users from `GET /api/user/{id}`, with `is_active` set to `false`. Older
// ones return a 404 instead, in which case the user is looked up in the list of all users, which can be made to include
// deactivated ones. A deactivated user is never reactivated in order to make it readable: doing so during a refresh or
// an import would silently restore access for users that were deliberately deactivated.
func readUser(ctx context.Context, client *metabase.ClientWithResponses, id int) (*metabase.User, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	getResp, err := client.GetUserWithResponse(ctx, id)

	diags.Append(checkMetabaseResponse(getResp, err, []int{200, 404}, "get user")...)
	if diags.HasError() {
		return nil, false, diags
	}

	if getResp.StatusCode() == 200 {
		return getResp.JSON200, true, diags
	}

	status := allUsersStatus
	listResp, err := client.ListUsersWithResponse(ctx, &metabase.ListUsersParams{Status: &status})

	diags.Append(checkMetabaseResponse(listResp, err, []int{200}, "list users")...)
	if diags.HasError() {
		return nil, false, diags
	}

	for i := range listResp.JSON200.Data {
		if listResp.JSON200.Data[i].Id == id {
			return &listResp.JSON200.Data[i], true, diags
		}
	}

	return nil, false, diags
}

// Reads the user with the given ID from the Metabase API and copies it to the given model. Returns whether the user
// still exists.
func readUserIntoModel(ctx context.Context, client *metabase.ClientWithResponses, id int, data *UserResourceModel) (bool, diag.Diagnostics) {
	user, found, diags := readUser(ctx, client, id)
	if diags.HasError() || !found {
		return found, diags
	}

	diags.Append(updateModelFromUser(ctx, *user, data)...)
	return true, diags
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data *UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	memberships, diags := makeUserGroupMemberships(ctx, data.GroupIds, data.IsSuperuser)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createResp, err := r.client.CreateUserWithResponse(ctx, metabase.CreateUserBody{
		Email:                data.Email.ValueString(),
		FirstName:            valueStringOrNullIfUnknown(data.FirstName),
		LastName:             valueStringOrNullIfUnknown(data.LastName),
		UserGroupMemberships: &memberships,
	})

	if err == nil && createResp != nil && createResp.StatusCode() == 400 && strings.Contains(createResp.BodyString(), emailAlreadyInUseError) {
		resp.Diagnostics.AddError(
			"A Metabase user already exists with this email address.",
			fmt.Sprintf("Metabase never deletes users, it only deactivates them, and a deactivated user keeps its email "+
				"address. '%s' therefore belongs to an existing user, which may be deactivated.\n\n"+
				"This provider never reactivates a user implicitly. If the existing user should be managed by Terraform, "+
				"import it with `terraform import`, and set `is_active` to `true` if it should also be reactivated.",
				data.Email.ValueString()),
		)
		return
	}

	resp.Diagnostics.Append(checkMetabaseResponse(createResp, err, []int{200}, "create user")...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := createResp.JSON200.Id

	// `POST /api/user` does not accept a locale, it can only be set by updating the user afterwards.
	if !data.Locale.IsNull() && !data.Locale.IsUnknown() {
		updateResp, err := r.client.UpdateUserWithResponse(ctx, id, metabase.UpdateUserBody{
			Locale: valueStringOrNullIfUnknown(data.Locale),
		})

		resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "set user locale")...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Metabase always creates users in an active state. Creating a resource that explicitly declares itself inactive
	// deactivates it right away.
	if !data.IsActive.IsUnknown() && !data.IsActive.ValueBool() {
		resp.Diagnostics.Append(deactivateUser(ctx, r.client, id)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	found, diags := readUserIntoModel(ctx, r.client, id, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("The user could not be read back after being created.", fmt.Sprintf("User ID: %d", id))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data *UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, diags := readUserIntoModel(ctx, r.client, int(data.Id.ValueInt64()), data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Returns whether any attribute other than `is_active` differs between the state and the plan.
func userAttributesChanged(state *UserResourceModel, plan *UserResourceModel) bool {
	return !state.Email.Equal(plan.Email) ||
		!state.FirstName.Equal(plan.FirstName) ||
		!state.LastName.Equal(plan.LastName) ||
		!state.Locale.Equal(plan.Locale) ||
		!state.IsSuperuser.Equal(plan.IsSuperuser) ||
		!state.GroupIds.Equal(plan.GroupIds)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan *UserResourceModel
	var state *UserResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := int(plan.Id.ValueInt64())
	wasActive := state.IsActive.ValueBool()
	// `is_active` is optional and computed: when it is not set in the configuration, the plan carries the value from the
	// state, which means a deactivated user stays deactivated unless the configuration explicitly asks otherwise.
	shouldBeActive := plan.IsActive.ValueBool()

	if !wasActive && shouldBeActive {
		reactivateResp, err := r.client.ReactivateUserWithResponse(ctx, id)

		resp.Diagnostics.Append(checkMetabaseResponse(reactivateResp, err, []int{200}, "reactivate user")...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if wasActive || shouldBeActive {
		memberships, diags := makeUserGroupMemberships(ctx, plan.GroupIds, plan.IsSuperuser)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		updateResp, err := r.client.UpdateUserWithResponse(ctx, id, metabase.UpdateUserBody{
			Email:                valueStringOrNullIfUnknown(plan.Email),
			FirstName:            valueStringOrNullIfUnknown(plan.FirstName),
			LastName:             valueStringOrNullIfUnknown(plan.LastName),
			Locale:               valueStringOrNullIfUnknown(plan.Locale),
			IsSuperuser:          plan.IsSuperuser.ValueBoolPointer(),
			UserGroupMemberships: &memberships,
		})

		resp.Diagnostics.Append(checkMetabaseResponse(updateResp, err, []int{200}, "update user")...)
		if resp.Diagnostics.HasError() {
			return
		}
	} else if userAttributesChanged(state, plan) {
		resp.Diagnostics.AddError(
			"A deactivated Metabase user cannot be updated.",
			"Metabase rejects updates to deactivated users. Set `is_active` to `true` to reactivate the user, which is "+
				"an explicit and auditable change, and apply the remaining changes afterwards.",
		)
		return
	}

	if !shouldBeActive && wasActive {
		resp.Diagnostics.Append(deactivateUser(ctx, r.client, id)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	found, diags := readUserIntoModel(ctx, r.client, id, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !found {
		resp.Diagnostics.AddError("The user could not be read back after being updated.", fmt.Sprintf("User ID: %d", id))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Deactivates the user with the given ID. Metabase does not support permanently deleting users.
func deactivateUser(ctx context.Context, client *metabase.ClientWithResponses, id int) diag.Diagnostics {
	deactivateResp, err := client.DeactivateUserWithResponse(ctx, id)

	return checkMetabaseResponse(deactivateResp, err, []int{200, 404}, "deactivate user")
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data *UserResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(deactivateUser(ctx, r.client, int(data.Id.ValueInt64()))...)
}

func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importStatePassthroughIntegerId(ctx, req, resp)
}
