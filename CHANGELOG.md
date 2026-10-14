## Unreleased

NEW FEATURES:

- Add the `metabase_collection_permission` resource, which manages a single (group, collection) pair of the collection graph, and leaves all other pairs untouched. Unlike the `metabase_collection_graph` resource, it can coexist with permissions set in the Metabase interface, much like the `google_*_iam_member` resources compared to `google_*_iam_policy`. The permission can be set to `none`, e.g. to make sure a group has no access to a collection, as new collections inherit the permissions of their parent collection. Deleting the resource sets the permission to `none`, like removing the pair from the graph resource.
- Add the `metabase_database_permission` resource, which manages a single (group, database) pair of the permissions graph, and leaves all other pairs untouched. Unlike the `metabase_permissions_graph` resource, it can coexist with permissions set in the Metabase interface, much like the `google_*_iam_member` resources compared to `google_*_iam_policy`. The lowest permissions can be declared, e.g. to make sure a group has no access to a database. Deleting the resource revokes the permissions of its pair, like removing the pair from the graph resource.

BUG FIXES:

- Read the `metabase_permissions_graph` in linear time. Each (group, database) pair returned by Metabase was matched against the existing state with a linear scan, which made reading large graphs (e.g. 126 groups and 100 databases) quadratic. (Thanks @samssh!)

## 0.18.0 (2026-10-13)

NEW FEATURES:

- Add the `metabase_collections` data source, listing all the collections along with a computed human-readable `path` and an `ids_by_path` lookup map. (Thanks @samssh!)

BUG FIXES:

- Summarize the body of unexpected Metabase API responses in error messages. Metabase 500 responses carry a large debug trace that can include submitted values such as database passwords. Only the `cause` or `message` is now reported, and other bodies are truncated. (Thanks @samssh!)
- Keep the planned `custom_details` of a `metabase_database` after it is created or updated. Metabase can alter values when saving (e.g. `ssl`), which made the apply fail with an "inconsistent values for sensitive attribute" error and left the database tainted. Such changes are now reported as an in-place update on the next plan. (Thanks @samssh!)
- Apply the removal of the `description` or `parent_id` of a `metabase_collection`. Null values were omitted from the update request, which Metabase interprets as leaving the attributes unchanged, so moving a collection to the root collection or removing its description failed with an inconsistent result error. (Thanks @ledmonster!)
- Serialize the creation and update of `metabase_collection` resources. Metabase records a collection graph revision when a collection is created, or moved in a way that changes its permissions (e.g. out of a personal collection), and doing so for several collections in parallel could fail with a duplicate key error. Collections created through different provider configurations (e.g. aliases) are not serialized, as Terraform runs them in separate provider processes. (Thanks @ledmonster!)
- Update the `metabase_collection_graph` in the same apply as the creation of collections. Metabase rejects graph updates based on an outdated revision, and creating or moving a collection records a new one. The provider now ignores the revisions caused by its own requests, while updates are still rejected when the graph is changed by anything else. Graph updates are also serialized with the creation and update of collections, which could otherwise fail with a duplicate key error. Permissions inherited by collections created in the same apply are reported as drift on the next plan, rather than failing the apply. Collections can still be created and updated without admin permissions, in which case the revisions are not tracked, as reading them requires admin permissions. (Thanks @ledmonster!)
- Revoke the permissions of the (group, database) pairs removed from `metabase_permissions_graph`. Metabase never removes a pair from the graph, and rejected the request sent by the provider. The removed pairs now have their `create_queries` and `download` permissions revoked (as well as `data_model` and `details` with advanced permissions), and `view_data` set to `unrestricted`. Pairs with revoked permissions are considered absent when reading the graph. Applies which previously failed because of pairs that are not in the configuration now succeed, and revoke the permissions of those pairs: check the plan before upgrading. (Thanks @ledmonster!)
- Update the `metabase_permissions_graph` in the same apply as the creation of databases or groups. Metabase grants them default permissions, which made the apply fail with an inconsistent result. Those permissions are now reported as drift on the next plan, and revoked by the next apply if they are not part of the configuration. (Thanks @ledmonster!)
- Reference fields and cards through Terraform resources in the cards generated by `mbtf` for queries using MBQL 5, such that the generated configuration can be applied to a Metabase instance other than the one it was imported from. The field IDs in `["field", {options}, <id>]` clauses and the card IDs in `source-card` attributes were left as is, as well as the `card__<id>` values of `source-table` attributes in legacy queries. A card which is only used as the source of another card is now imported along with it, even if it is not part of any dashboard. `mbtf` no longer ignores the errors met when importing the table or the card a query is based on either: an import which used to complete with IDs left in place now either replaces them or fails. (Thanks @dtatarkin!)

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
