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
