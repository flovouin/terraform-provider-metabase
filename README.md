# Terraform Metabase provider

This repository contains the source code for the [Metabase](https://www.metabase.com/) Terraform provider.

For how to use the provider in a Terraform project, please refer to the [Terraform registry documentation](https://registry.terraform.io/providers/zerogachis/metabase/latest/docs).

## Metabase version compatibility

Unfortunately, this provider relies on the Metabase API which is [subject to breaking changes and not versioned](https://www.metabase.com/docs/latest/api-documentation#about-the-metabase-api). This makes it hard for this provider to keep up with Metabase versions, apologies for that. Here is a table that summarizes supported Metabase versions:

| Provider version \ Metabase version | .44 | .45 | .46 | .47 | .48 | .49 | .50 |
| ----------------------------------: | :-: | :-: | :-: | :-: | :-: | :-: | :-: |
|                              <= 0.3 | ✅  | ❌  | ❌  | ❌  | ❌  | ❌  | ❌  |
|                       >= 0.4, < 0.8 | ❌  | ❌  | ❌  | ❌  | ✅  | ✅  | ❌  |
|                              >= 0.8 | ❌  | ❌  | ❌  | ❌  | ❌  | ❌  | ✅  |

## 🔨 `mbtf` importer tool

The `mbtf` command line allows importing Metabase dashboards and cards from a Metabase instance (API).

### Installation

Currently, the tool can be installed using the `go` command (binaries will be provided as part of each release in the future):

```bash
export PATH=${PATH}:$(go env GOPATH)/bin
cd cmd/mbtf
go install
```

### Running

Simply run the `mbtf` utility with no argument. A configuration should be defined first (see next section).

```bash
mbtf
```

Running the tool will connect to the Metabase API, list all dashboards matching the filter defined in the configuration, and import the dashboards and cards as Terraform files.

### Configuration

`mbtf` is configured using a single YAML file that should be located in the current directory where `mbtf` is run, with the name `mbtf.yml`. This file has the following structure:

```yaml
# Configures how to connect to Metabase.
metabase:
  # The URL to the Metabase API. Usually suffixed with `/api`.
  endpoint: http://metabase-endpoint.com/api
  # The following parameters can (and probably should) be defined as environment variables `MBTF_METABASE_USERNAME` and
  # `MBTF_METABASE_PASSWORD`.
  username: email@address.com
  password: password

# Databases are not imported by `mbtf` and should already be defined in the Terraform configuration.
# This defines how the mapping is made between databases found in the Metabase API and Terraform.
databases:
  mapping:
    # A database name `Big Query` in the Metabase API will be referenced as `metabase_database.bigquery` in the
    # imported cards and dashboards.
    - name: Big Query
      resource_name: bigquery
    # A database can also be referenced by its ID in the Metabase API.
    - id: 23
      resource_name: postgres

# Similarly to databases, collections are not imported by `mbtf` and a mapping between the Metabase API and Terraform
# should be provided.
collections:
  mapping:
    # The collection with ID `193` in the Metabase API will be referenced as `metabase_collection.my_collection` in the
    # imported cards and dashboards. The `id` can also be `root` for the default collection.
    - id: 193
      resource_name: my_collection
    # A collection can also be referenced by its name in Metabase.
    - name: Other collection
      resource_name: other_collection

# Determines which dashboards should be imported.
dashboard_filter:
  # The list of collections for which dashboards should be imported.
  # All collections are imported by default if this is not specified.
  included_collections:
    - id: 53 # Just like for collections mapping, this can be `root` to include the default collection.
  # The list of collections to exclude from the import. This takes precedence over `included_collections`. However,
  # there is no point to define both included and excluded collections.
  excluded_collections:
    # Collections can also be filtered by name.
    - name: Private collection

  # A regexp that the dashboard name should match in order to be imported.
  dashboard_name: ^\[Public\]
  # A regexp that the dashboard description should match in order to be imported.
  dashboard_description: tag:reviewed

  # The list of IDs of the dashboards to import. If this is non-empty, all other parameters are ignored.
  dashboard_ids: [2, 3, 4]

# Defines how the Terraform configuration is written to files.
output:
  # The path where the Terraform configuration will be written.
  path: ./metabase
  # Whether generated files matching `mb-gen-*.tf` should be removed from the output directory before writing.
  clear: true
  # When `true`, `terraform fmt` is not called after writing the Terraform files.
  disable_formatting: false
```

## 🧑‍💻 Development

### Provider source

The source code provider is mainly located in the [internal/provider](./internal/provider/) folder. Each resource and data source has its own source file.

### Metabase API client

The source for the Metabase API client is located in the [metabase](./metabase/) folder and is mainly generated automatically from the [OpenAPI definition](./metabase-api.yaml). When updating the definition, the client can be regenerated using the `go generate` command.

### `mbtf` importer

The source for the Metabase to Terraform importer is located in the [internal/importer](./internal/importer/) folder, and its entrypoint is in [cmd/mbtf](./cmd/mbtf/). The `mbtf` importer uses the Metabase API client to retrieve resources and convert them to Terraform definitions.
