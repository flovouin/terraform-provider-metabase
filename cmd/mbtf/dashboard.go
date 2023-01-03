package main

import (
	"context"
	"errors"
	"regexp"

	"github.com/flovouin/terraform-provider-metabase/metabase"
)

// Returns whether the given collection matches any of the definitions.
// Only collections with an integer ID are supported.
func isCollectionInDefinitions(c metabase.Collection, definitions []collectionDefinition) (bool, error) {
	collectionId, err := c.Id.AsCollectionId1()
	if err != nil {
		// An error is not returned because we assume that the conversion failed because the ID is a string.
		return false, nil
	}

	for _, d := range definitions {
		if d.Id > 0 && collectionId == d.Id {
			return true, nil
		}

		if len(d.Name) > 0 {
			matchName, err := regexp.MatchString(d.Name, c.Name)
			if err != nil {
				return false, err
			}

			if matchName {
				return true, nil
			}
		}
	}

	return false, nil
}

// Returns the list of collections for which dashboard should be imported.
// The second argument indicates whether the root (`null`) collection should be imported as well.
func listCollectionsToImport(ctx context.Context, config dashboardFilterConfig, client metabase.ClientWithResponses) (map[int]bool, bool, error) {
	listResp, err := client.ListCollectionsWithResponse(ctx, &metabase.ListCollectionsParams{})
	if err != nil {
		return nil, false, err
	}
	if listResp.JSON200 == nil {
		return nil, false, errors.New("received unexpected response when listing collections")
	}

	emptyIncludedCollectionsList := len(config.IncludedCollections) == 0

	collectionIds := make(map[int]bool)
	for _, c := range *listResp.JSON200 {
		// The only collection with a non-integer ID is the "root" collection, and there's no point in including it as it is
		// never referenced in dashboards (`null` is used instead).
		id, err := c.Id.AsCollectionId1()
		if err != nil {
			continue
		}

		// Excluded collections take precedence over inclusion.
		isExcluded, err := isCollectionInDefinitions(c, config.ExcludedCollections)
		if err != nil {
			return nil, false, err
		}
		if isExcluded {
			continue
		}

		isIncluded := true
		if !emptyIncludedCollectionsList {
			isIncluded, err = isCollectionInDefinitions(c, config.IncludedCollections)
			if err != nil {
				return nil, false, err
			}
		}

		if isIncluded {
			collectionIds[id] = true
		}
	}

	return collectionIds, emptyIncludedCollectionsList, nil
}

// Fetches all dashboards from Metabase and returns the list of IDs of dashboards that should be imported.
func listDashboardsToImport(ctx context.Context, config dashboardFilterConfig, client metabase.ClientWithResponses) ([]int, error) {
	if len(config.DashboardIds) > 0 {
		return config.DashboardIds, nil
	}

	collectionIds, includeNilCollection, err := listCollectionsToImport(ctx, config, client)
	if err != nil {
		return nil, err
	}

	var nameRegexp *regexp.Regexp
	if len(config.DashboardName) > 0 {
		r, err := regexp.Compile(config.DashboardName)
		if err != nil {
			return nil, err
		}

		nameRegexp = r
	}

	var descriptionRegexp *regexp.Regexp
	if len(config.DashboardDescription) > 0 {
		r, err := regexp.Compile(config.DashboardDescription)
		if err != nil {
			return nil, err
		}

		descriptionRegexp = r
	}

	listResp, err := client.ListDashboardsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	if listResp.JSON200 == nil {
		return nil, errors.New("received unexpected response when listing dashboards")
	}

	dashboardIds := make([]int, 0)
	for _, dashboard := range *listResp.JSON200 {
		if (dashboard.CollectionId == nil && !includeNilCollection) ||
			(dashboard.CollectionId != nil && !collectionIds[*dashboard.CollectionId]) {
			continue
		}

		if nameRegexp != nil && !nameRegexp.MatchString(dashboard.Name) {
			continue
		}

		if descriptionRegexp != nil &&
			(dashboard.Description == nil || !descriptionRegexp.MatchString(*dashboard.Description)) {
			continue
		}

		dashboardIds = append(dashboardIds, dashboard.Id)
	}

	return dashboardIds, nil
}
