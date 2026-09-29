package provider

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var providerConfig = fmt.Sprintf(`
provider "metabase" {
  endpoint = "%s"
  username = "%s"
  password = "%s"
}
`,
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_USERNAME"),
	os.Getenv("METABASE_PASSWORD"),
)

var providerApiKeyConfig = fmt.Sprintf(`
provider "metabase" {
	endpoint = "%s"
	api_key = "%s"
}
`,
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_API_KEY"),
)

// The schema the tables of the sample database belong to. Until 0.62, Metabase bundled an H2 sample database, in which
// tables belong to the `PUBLIC` schema. From 0.63 onwards, the sample database is a SQLite one, in which tables belong
// to no schema at all. The test setup detects it and sets `METABASE_SAMPLE_SCHEMA` accordingly, an empty value being a
// valid (schema-less) result. The `PUBLIC` fallback only applies when the variable is not set at all.
var sampleDatabaseSchema = func() string {
	if schema, ok := os.LookupEnv("METABASE_SAMPLE_SCHEMA"); ok {
		return schema
	}

	return "PUBLIC"
}()

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"metabase": providerserver.NewProtocol6WithError(New("test")()),
}

var testAccMetabaseClient, _ = metabase.MakeAuthenticatedClientWithUsernameAndPassword(
	context.Background(),
	os.Getenv("METABASE_URL"),
	os.Getenv("METABASE_USERNAME"),
	os.Getenv("METABASE_PASSWORD"),
)

// Returns a provider configuration in which all values are known.
func makeKnownProviderConfig() MetabaseProviderModel {
	return MetabaseProviderModel{
		Endpoint: types.StringValue("https://metabase.example.com/api"),
		Username: types.StringValue("user@tests.com"),
		Password: types.StringValue("password"),
		ApiKey:   types.StringNull(),
		ExtraHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{
			"CF-Access-Client-Id": types.StringValue("client-id"),
		}),
	}
}

func TestValidateProviderConfigIsKnown(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		update        func(data *MetabaseProviderModel)
		expectedPaths []string
	}{
		"known values": {
			update: func(data *MetabaseProviderModel) {},
		},
		"null optional values": {
			update: func(data *MetabaseProviderModel) {
				data.Username = types.StringNull()
				data.Password = types.StringNull()
				data.ApiKey = types.StringValue("api-key")
				data.ExtraHeaders = types.MapNull(types.StringType)
			},
		},
		"unknown endpoint": {
			update: func(data *MetabaseProviderModel) {
				data.Endpoint = types.StringUnknown()
			},
			expectedPaths: []string{"endpoint"},
		},
		"unknown credentials": {
			update: func(data *MetabaseProviderModel) {
				data.Username = types.StringUnknown()
				data.Password = types.StringUnknown()
				data.ApiKey = types.StringUnknown()
			},
			expectedPaths: []string{"username", "password", "api_key"},
		},
		"unknown extra headers": {
			update: func(data *MetabaseProviderModel) {
				data.ExtraHeaders = types.MapUnknown(types.StringType)
			},
			expectedPaths: []string{"extra_headers"},
		},
		"unknown extra header value": {
			update: func(data *MetabaseProviderModel) {
				data.ExtraHeaders = types.MapValueMust(types.StringType, map[string]attr.Value{
					"CF-Access-Client-Id":     types.StringValue("client-id"),
					"CF-Access-Client-Secret": types.StringUnknown(),
				})
			},
			expectedPaths: []string{"extra_headers"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data := makeKnownProviderConfig()
			test.update(&data)

			diags := validateProviderConfigIsKnown(data)

			var paths []string
			for _, d := range diags.Errors() {
				if withPath, ok := d.(diag.DiagnosticWithPath); ok {
					paths = append(paths, withPath.Path().String())
				}
			}
			if len(paths) != len(diags) || !slices.Equal(paths, test.expectedPaths) {
				t.Fatalf("Expected errors on %v, got diagnostics: %v", test.expectedPaths, diags)
			}
		})
	}
}
