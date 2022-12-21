package metabase

// This file ensures the Metabase responses conform to the `MetabaseResponse` interface, for convenience when processing
// them during Terraform operations.
type MetabaseResponse interface {
	StatusCode() int
	BodyString() string
}
