package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/flovouin/terraform-provider-metabase/metabase"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
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
