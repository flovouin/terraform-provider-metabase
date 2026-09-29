package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// Ensures provider defined types fully satisfy framework interfaces.
var _ provider.Provider = &MetabaseProvider{}

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
	ApiKey   types.String `tfsdk:"api_key"`  // The API key to use to authenticate. This can be used instead of a user name and password.
	// Additional HTTP headers sent with every request to the Metabase API.
	ExtraHeaders types.Map `tfsdk:"extra_headers"`
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
				Optional:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "The password to use to authenticate.",
				Optional:            true,
				Sensitive:           true,
			},
			"api_key": schema.StringAttribute{
				MarkdownDescription: "The API key to use to authenticate. This can be used instead of a user name and password.",
				Optional:            true,
				Sensitive:           true,
			},
			"extra_headers": schema.MapAttribute{
				MarkdownDescription: "Additional HTTP headers to send with every request to the Metabase API. Useful when Metabase sits behind a proxy that expects credentials of its own, for example a Cloudflare Access service token (`CF-Access-Client-Id` and `CF-Access-Client-Secret`).",
				ElementType:         types.StringType,
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

// Returns an error for each provider attribute whose value is unknown, which happens when it depends on a value only
// known after apply. The Metabase client cannot be created from such values.
func validateProviderConfigIsKnown(data MetabaseProviderModel) diag.Diagnostics {
	var diags diag.Diagnostics

	extraHeadersAreUnknown := data.ExtraHeaders.IsUnknown()
	for _, value := range data.ExtraHeaders.Elements() {
		extraHeadersAreUnknown = extraHeadersAreUnknown || value.IsUnknown()
	}

	attributes := []struct {
		name      string
		isUnknown bool
	}{
		{"endpoint", data.Endpoint.IsUnknown()},
		{"username", data.Username.IsUnknown()},
		{"password", data.Password.IsUnknown()},
		{"api_key", data.ApiKey.IsUnknown()},
		{"extra_headers", extraHeadersAreUnknown},
	}
	for _, attribute := range attributes {
		if !attribute.isUnknown {
			continue
		}

		diags.AddAttributeError(
			path.Root(attribute.name),
			"Unknown provider configuration value",
			fmt.Sprintf("%s must be known when configuring the provider, but it depends on a value only known after apply. Either set it statically, or apply the resources it depends on first (e.g. using -target).", attribute.name),
		)
	}

	return diags
}

// Returns the client options corresponding to the extra headers set on the provider, if any. The headers are set on
// every request, including the session request made when authenticating with a username and password.
func makeClientOptions(ctx context.Context, extraHeaders types.Map) ([]metabase.ClientOption, diag.Diagnostics) {
	var diags diag.Diagnostics

	if extraHeaders.IsNull() {
		return nil, diags
	}

	headers := make(map[string]string, len(extraHeaders.Elements()))
	diags.Append(extraHeaders.ElementsAs(ctx, &headers, false)...)
	if diags.HasError() {
		return nil, diags
	}

	return []metabase.ClientOption{
		metabase.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			for name, value := range headers {
				req.Header.Set(name, value)
			}
			return nil
		}),
	}, diags
}

func (p *MetabaseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data MetabaseProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(validateProviderConfigIsKnown(data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var err error
	var authenticatedClient *metabase.ClientWithResponses

	clientOptions, diags := makeClientOptions(ctx, data.ExtraHeaders)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !data.Username.IsNull() && !data.Password.IsNull() {
		if !data.ApiKey.IsNull() {
			resp.Diagnostics.AddError("Only one of username / password or API key can be provided.", "")
			return
		}

		authenticatedClient, err = metabase.MakeAuthenticatedClientWithUsernameAndPassword(
			ctx,
			data.Endpoint.ValueString(),
			data.Username.ValueString(),
			data.Password.ValueString(),
			clientOptions...,
		)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create the Metabase client from username and password.", err.Error())
			return
		}
	} else if !data.ApiKey.IsNull() {
		if !data.Username.IsNull() || !data.Password.IsNull() {
			resp.Diagnostics.AddError("Only one of username / password or API key can be provided.", "")
			return
		}

		authenticatedClient, err = metabase.MakeAuthenticatedClientWithApiKey(
			ctx,
			data.Endpoint.ValueString(),
			data.ApiKey.ValueString(),
			clientOptions...,
		)
		if err != nil {
			resp.Diagnostics.AddError("Failed to create the Metabase client from the API key.", err.Error())
			return
		}
	} else {
		resp.Diagnostics.AddError("Either username / password or API key must be provided.", "")
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
		NewContentTranslationResource,
		NewDashboardResource,
		NewDatabaseResource,
		NewPermissionsGraphResource,
		NewPermissionsGroupResource,
		NewTableResource,
		NewUserResource,
	}
}

func (p *MetabaseProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewCollectionGraphDataSource,
		NewDatabaseDataSource,
		NewPermissionsGraphDataSource,
		NewTableDataSource,
		NewUserDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &MetabaseProvider{
			version: version,
		}
	}
}
