package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/zerogachis/terraform-provider-metabase/metabase"
)

// Returns the collection ID as a string, possibly converting it from an integer.
func collectionIdAsString(id metabase.Collection_Id) (*string, error) {
	idStr, err := id.AsCollectionId0()
	if err == nil {
		return &idStr, nil
	}

	idInt, err := id.AsCollectionId1()
	if err != nil {
		return nil, err
	}

	idStr = fmt.Sprintf("%d", idInt)
	return &idStr, nil
}

// Returns whether the given collection matches any of the definitions.
func isCollectionInDefinitions(c metabase.Collection, definitions []collectionDefinition) (bool, error) {
	collectionId, err := collectionIdAsString(c.Id)
	if err != nil {
		// An error is not returned because we assume that the conversion failed because the ID is a string.
		return false, nil
	}

	for _, d := range definitions {
		if d.Id != "" && *collectionId == d.Id {
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

// Returns the list of collections for which dashboards should be imported.
func listCollectionsToImport(ctx context.Context, config dashboardFilterConfig, client metabase.ClientWithResponses) ([]string, error) {
	listResp, err := client.ListCollectionsWithResponse(ctx, &metabase.ListCollectionsParams{})
	if err != nil {
		return nil, err
	}
	if listResp.JSON200 == nil {
		return nil, errors.New("received unexpected response when listing collections")
	}

	emptyIncludedCollectionsList := len(config.IncludedCollections) == 0

	collectionIds := make([]string, 0, len(*listResp.JSON200))
	for _, c := range *listResp.JSON200 {
		id, err := collectionIdAsString(c.Id)
		if err != nil {
			continue
		}

		// Excluded collections take precedence over inclusion.
		isExcluded, err := isCollectionInDefinitions(c, config.ExcludedCollections)
		if err != nil {
			return nil, err
		}
		if isExcluded {
			continue
		}

		isIncluded := true
		if !emptyIncludedCollectionsList {
			isIncluded, err = isCollectionInDefinitions(c, config.IncludedCollections)
			if err != nil {
				return nil, err
			}
		}

		if isIncluded {
			collectionIds = append(collectionIds, *id)
		}
	}

	return collectionIds, nil
}

// Fetches all dashboards from Metabase and returns the list of IDs of dashboards that should be imported.
func listDashboardsToImport(ctx context.Context, config dashboardFilterConfig, client metabase.ClientWithResponses) ([]int, error) {
	if len(config.DashboardIds) > 0 {
		return config.DashboardIds, nil
	}

	collectionIds, err := listCollectionsToImport(ctx, config, client)
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

	dashboardIds := make([]int, 0)

	for _, collectionId := range collectionIds {
		listResp, err := client.ListCollectionItemsWithResponse(ctx, collectionId, &metabase.ListCollectionItemsParams{
			Models: &[]metabase.CollectionItemModel{metabase.CollectionItemModelDashboard},
		})
		if err != nil {
			return nil, err
		}
		if listResp.JSON200 == nil {
			return nil, errors.New("received unexpected response when listing dashboards")
		}
		if listResp.JSON200.Total != len(listResp.JSON200.Data) {
			return nil, errors.New("received unexpected response when listing dashboards: pagination is not supported")
		}

		for _, dashboard := range listResp.JSON200.Data {
			if nameRegexp != nil && !nameRegexp.MatchString(dashboard.Name) {
				continue
			}

			if descriptionRegexp != nil &&
				(dashboard.Description == nil || !descriptionRegexp.MatchString(*dashboard.Description)) {
				continue
			}

			dashboardIds = append(dashboardIds, dashboard.Id)
		}
	}

	return dashboardIds, nil
}
