package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/deepmap/oapi-codegen/pkg/securityprovider"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ provider.ProviderWithSchema = &MetabaseProvider{}
var _ provider.ProviderWithMetadata = &MetabaseProvider{}

// Handles Metabase-related resources.
type MetabaseProvider struct {
	// Version is set to the provider version on release, "dev" when the provider is built and ran locally, and "test"
	// when running acceptance testing.
	version string
}

// The Terraform model for the provider.
type MetabaseProviderModel struct {
	Endpoint types.String `tfsdk:"endpoint"` // The URL to the Metabase API.
	Username types.String `tfsdk:"username"` // The user name (or email address) to use to authenticate.
	Password types.String `tfsdk:"password"` // The password to use to authenticate.
}

func (p *MetabaseProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "metabase"
	resp.Version = p.version
}

func (p *MetabaseProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: `The Metabase provider allows managing both metadata (collections, permissions groups) and actual visualizations (cards/questions and dashboards).

While most Terraform resources fully define the Metabase objects using attributes, the most complex ones (cards and dashboards) must be defined using JSON (and possibly templates).`,

		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "The URL to the Metabase API.",
				Required:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "The user name (or email address) to use to authenticate.",
				Required:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "The password to use to authenticate.",
				Required:            true,
				Sensitive:           true,
			},
		},
	}
}

// Authenticates to the Metabase API using the given username and password, and returns an API client configured with
// the session obtained during authentication.
func makeAuthenticatedClient(ctx context.Context, endpoint string, username string, password string) (*metabase.ClientWithResponses, diag.Diagnostics) {
	var diags diag.Diagnostics

	client, err := metabase.NewClientWithResponses(endpoint)
	if err != nil {
		diags.AddError("Error when creating the Metabase client.", err.Error())
		return nil, diags
	}

	sessionResp, err := client.CreateSessionWithResponse(ctx, metabase.CreateSessionBody{
		Username: username,
		Password: password,
	})

	diags.Append(checkMetabaseResponse(sessionResp, err, []int{200}, "create session")...)
	if diags.HasError() {
		return nil, diags
	}

	// Authenticated calls are made by passing the session ID in a Metabase-specific header.
	apiKeyProvider, err := securityprovider.NewSecurityProviderApiKey("header", "X-Metabase-Session", sessionResp.JSON200.Id)
	if err != nil {
		diags.AddError("Error while creating the authenticated Metabase client.", err.Error())
		return nil, diags
	}

	authenticatedClient, err := metabase.NewClientWithResponses(endpoint, metabase.WithRequestEditorFn(apiKeyProvider.Intercept))
	if err != nil {
		diags.AddError("Error while creating the authenticated Metabase client.", err.Error())
		return nil, diags
	}

	return authenticatedClient, diags
}

func (p *MetabaseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data MetabaseProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	authenticatedClient, diags := makeAuthenticatedClient(ctx, data.Endpoint.ValueString(), data.Username.ValueString(), data.Password.ValueString())
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.DataSourceData = authenticatedClient
	resp.ResourceData = authenticatedClient
}

func (p *MetabaseProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewCardResource,
		NewCollectionGraphResource,
		NewCollectionResource,
		NewDashboardResource,
		NewDatabaseResource,
		NewPermissionsGraphResource,
		NewPermissionsGroupResource,
	}
}

func (p *MetabaseProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewTableDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MetabaseProvider{
			version: version,
		}
	}
}
