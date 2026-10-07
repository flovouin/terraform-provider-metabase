## Unreleased

BUG FIXES:

- Apply the removal of the `description` or `parent_id` of a `metabase_collection`. Null values were omitted from the update request, which Metabase interprets as leaving the attributes unchanged, so moving a collection to the root collection or removing its description failed with an inconsistent result error.
- Serialize the creation and update of `metabase_collection` resources. Metabase records a collection graph revision when a collection is created, or moved in a way that changes its permissions (e.g. out of a personal collection), and doing so for several collections in parallel could fail with a duplicate key error. Collections created through different provider configurations (e.g. aliases) are not serialized, as Terraform runs them in separate provider processes.
- Update the `metabase_collection_graph` in the same apply as the creation of collections. Metabase rejects graph updates based on an outdated revision, and creating or moving a collection records a new one. The provider now ignores the revisions caused by its own requests, while updates are still rejected when the graph is changed by anything else. Graph updates are also serialized with the creation and update of collections, which could otherwise fail with a duplicate key error. Permissions inherited by collections created in the same apply are reported as drift on the next plan, rather than failing the apply. Collections can still be created and updated without admin permissions, in which case the revisions are not tracked, as reading them requires admin permissions.
- Revoke the permissions of the (group, database) pairs removed from `metabase_permissions_graph`. Metabase never removes a pair from the graph, and rejected the request sent by the provider. The removed pairs now have their `create_queries` and `download` permissions revoked (as well as `data_model` and `details` with advanced permissions), and `view_data` set to `unrestricted`. Pairs with revoked permissions are considered absent when reading the graph. Applies which previously failed because of pairs that are not in the configuration now succeed, and revoke the permissions of those pairs: check the plan before upgrading.

## 0.17.0 (2026-09-29)

NEW FEATURES:

- Add the `extra_headers` provider attribute, a map of HTTP headers sent with every request to the Metabase API. This allows the provider to reach a Metabase instance behind a proxy that expects credentials of its own, such as a Cloudflare Access service token. The headers are also sent on the session request made when authenticating with a username and password. (Thanks @PJHRobles!)

BUG FIXES:

- Report an explicit error when a provider attribute (`endpoint`, `username`, `password`, `api_key` or `extra_headers`) is unknown when configuring the provider.

## 0.16.0 (2026-09-26)

NEW FEATURES:

- Add the `metabase_user` resource and the `metabase_user` data source, allowing user accounts and their permissions group memberships to be managed as code. Metabase only deactivates users rather than deleting them, and this resource never reactivates one implicitly: a user deactivated outside of Terraform is reported with `is_active = false` by refreshes, plans and imports, and is only restored when `is_active = true` is set explicitly in the configuration. (Thanks @goakshit!)

## 0.15.0 (2026-08-08)

NEW FEATURES:

- Support write-only credentials in the `metabase_database` resource. `custom_details.sensitive_details_json_wo` accepts a JSON object that is merged into `details_json` before requests are sent to Metabase, but is never persisted in plans nor in the state. `custom_details.sensitive_details_json_wo_version` should be incremented to send rotated credentials to Metabase. Requires Terraform 1.11 or later. (Thanks @samssh!)

## 0.14.3 (2026-07-28)

BUG FIXES:

- Treat the map and list representations of `template-tags` in a `metabase_card` query as equivalent.

## 0.14.2 (2026-05-29)

BUG FIXES:

- Handle `metabase_permissions_graph` `create_queries` as either a simple string or a serialized JSON object. The Metabase API has accepted both shapes since 0.50.0 (`create-queries` shares the same `Schemas` union as `view-data`), but the provider was only modeling the scalar form, which caused `terraform plan` to fail with a JSON unmarshal error during state refresh whenever the server returned the object form. (Thanks @rajeshfintech!)

## 0.14.1 (2026-02-03)

BUG FIXES:

- Only update dashboard cards in the state if the API returns an order-independent difference.
- Fix broken card references in dashboard card visualization settings in `mbtf`. (Thanks @cschuff!)
- Sort cards by position in `mbtf` importer output for stable diffs. (Thanks @cschuff!)

## 0.14.0 (2026-01-28)

NEW FEATURES:

- Add the `metabase_database` data source, and support referencing them in `mbtf`. (Thanks @cschuff!)
- Support importing dashboard tabs in `mbtf`. (Thanks @gouglhupf and @cschuff!)
- Support replacing card parameter references in card configuration in `mbtf`. (Thanks @gouglhupf!)

## 0.13.2 (2026-01-14)

BUG FIXES:

- Fix inconsistent cards order in dashboard API result. (Thanks @TheMrZZ!)

## 0.13.1 (2026-01-13)

BUG FIXES:

- Fix inconsistent cards order in dashboard API result. (Thanks @TheMrZZ!)

## 0.13.0 (2025-12-21)

NEW FEATURES:

- Support tabs in dashboards. (Thanks @TheMrZZ!)

## 0.12.0 (2025-12-15)

NEW FEATURES:

- Add the `metabase_collection_graph` and `metabase_permissions_graph` data sources. (Thanks @TheMrZZ!)

## 0.11.0 (2025-12-14)

ENHANCEMENTS:

- Add support for API key authentication in `mbtf`.
- Handle card references in dashboard parameters. (Thanks @gouglhupf!)

## 0.10.1 (2025-12-07)

BUG FIXES:

- Fix card cleaning for native queries.

## 0.10.0 (2025-09-21)

NEW FEATURES:

- Add the `metabase_content_translation` resource. (#82, thanks @nicolasbriere1!)

## 0.9.0 (2025-06-29)

NEW FEATURES:

- Handle `metabase_permissions_graph` `view_data` as either a simple string or a serialized JSON object.

BUG FIXES:

- Ignore `aggregation-idents` and `breakout-idents` in the response when they are not part of the card model.

## 0.8.1 (2024-09-22)

BUG FIXES:

- Catch and return an error when the API returns a response that could not be parsed.

## 0.8.0 (2024-09-08)

BREAKING CHANGES:

- Support Metabase v\*.50, and drop support for earlier versions.
- `metabase_permissions_graph`'s permissions support two new fields: `view_data` and `create_queries`. The `native` field is no longer supported.

## 0.7.0 (2024-06-27)

NEW FEATURES:

- Support any parameters in `metabase_dashboard`'s `parameters_json`. (#60, thanks @michal-billtech!)

## 0.6.0 (2024-04-16)

NEW FEATURES:

- Support [linking filters](https://www.metabase.com/learn/dashboards/linking-filters) (aka filtering parameters in the API).

## 0.5.1 (2024-04-07)

BUG FIXES:

- Ignore unsupported granular permissions rather than crashing because of an unexpected Metabase API response. (#49, thanks @ellingtonjp!)
- Ignore permissions for the Metabase Analytics database (Pro feature), for which granular permissions are always set.

## 0.5.0 (2024-04-03)

NEW FEATURES:

- Support authentication using an [API key](https://www.metabase.com/docs/master/people-and-groups/api-keys).

## 0.4.0 (2024-01-31)

BREAKING CHANGES:

- Support Metabase v\*.48, and drop support for earlier versions. Make sure dashboard definitions follow the new schema (e.g. cards' `size{X|Y}` become `size_{x|y}`).
- Remove the `color` attribute on the `metabase_collection` resource.
- Remove the `cards_ids` attribute on the `metabase_dashboard` resource.

## 0.3.0 (2023-01-06)

NEW FEATURES:

- Introduce the `metabase_table` resource.
- Format Terraform files generated by `mbtf` automatically.

ENHANCEMENTS:

- The `metabase_table` data source now supports the `description` attribute.
- Use the `metabase_table` resource instead of data source in `mbtf`.

## 0.2.0 (2023-01-05)

NEW FEATURES:

- First version of the `mbtf` utility to import dashboard and cards from Metabase to Terraform.

ENHANCEMENTS:

- The `metabase_database` resource now supports any engine type through the `custom_details` attribute.

## 0.1.0 (2022-12-22)

NEW FEATURES:

- Introduce the `metabase_permissions_group` resource.
- Introduce the `metabase_collection` resource.
- Introduce the `metabase_database` resource.
- Introduce the `metabase_table` data source.
- Introduce the `metabase_collection_graph` resource.
- Introduce the `metabase_permissions_graph` resource.
- Introduce the `metabase_card` resource.
- Introduce the `metabase_dashboard` resource.
