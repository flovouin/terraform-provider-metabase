package metabase

// The default ID of the `Administrators` permissions group, created automatically by Terraform.
const AdministratorsPermissionsGroupId = 2

// The ID of the `Metabase Analytics` database, automatically created for pro plans.
const MetabaseAnalyticsDatabaseId = "13371337"

// The list of JSON attributes in a `Card` object that are needed to fully define the card, e.g. when creating it.
var DefiningCardAttributes = map[string]bool{
	"cache_ttl":              true,
	"collection_id":          true,
	"collection_position":    true,
	"dataset_query":          true,
	"description":            true,
	"display":                true,
	"name":                   true,
	"parameter_mappings":     true,
	"parameters":             true,
	"query_type":             true,
	"visualization_settings": true,
}

// The name of the attribute in cards for which the value is the ID of a `Table` object.
const SourceTableAttribute = "source-table"

// The name of the literal in an array, indicating a reference to a `Field` object.
const FieldLiteral = "field"

// The name of the literal in an array indicating a reference to a `Field` object in the next array element.
const FieldReferenceLiteral = "ref"

// The name of the attribute in cards which defines the database query.
const DatasetQueryAttribute = "dataset_query"

// The name of the attribute referencing a database ID.
const DatabaseAttribute = "database"

// The name of the attribute referencing the visualization settings in a `Card` object.
const VisualizationSettingsAttribute = "visualization_settings"

// The name of the attribute referencing the columns settings in a `Card` object's visualization settings.
const ColumnSettingsAttribute = "column_settings"

// The name of the attribute which references the parent collection in a card.
const CollectionIdAttribute = "collection_id"

// The name of the attribute referencing a card in a dashboard.
const CardIdAttribute = "card_id"

// The name of the attribute describing how dashboard parameters map to a specific card.
const ParameterMappingsAttribute = "parameter_mappings"

// The name of the attribute describing the target of a dashboard parameter for a specific card in the dashboard.
const TargetAttribute = "target"
