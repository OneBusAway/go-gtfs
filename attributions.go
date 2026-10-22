package gtfs

import (
	"fmt"
	"strings"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Attribution credits an organization with a role in the dataset, from
// attributions.txt. At most one of Agency, Route and Trip is set; when none
// is, the attribution applies to the whole dataset.
type Attribution struct {
	Id               string
	Agency           *Agency
	Route            *Route
	Trip             *ScheduledTrip
	OrganizationName string
	IsProducer       bool
	IsOperator       bool
	IsAuthority      bool
	Url              string
	Email            string
	Phone            string
}

// parseAttributions reads attributions.txt. A row is skipped with a warning
// if organization_name is empty, a role is not empty, 0 or 1, more than one
// scope is set, a scope ID is unknown, or attribution_id repeats an earlier
// one. An unknown ID must not become a nil scope, which would widen the
// attribution to the whole dataset.
func parseAttributions(csv *csv.File, agencies []Agency, routeIdToRoute map[string]*Route, tripIdToTrip map[string]*ScheduledTrip) ([]Attribution, []warnings.StaticWarning) {
	idColumn := csv.OptionalColumn("attribution_id")
	agencyIDColumn := csv.OptionalColumn("agency_id")
	routeIDColumn := csv.OptionalColumn("route_id")
	tripIDColumn := csv.OptionalColumn("trip_id")
	organizationNameColumn := csv.RequiredColumn("organization_name")
	isProducerColumn := csv.OptionalColumn("is_producer")
	isOperatorColumn := csv.OptionalColumn("is_operator")
	isAuthorityColumn := csv.OptionalColumn("is_authority")
	urlColumn := csv.OptionalColumn("attribution_url")
	emailColumn := csv.OptionalColumn("attribution_email")
	phoneColumn := csv.OptionalColumn("attribution_phone")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	agencyIdToAgency := agenciesByID(agencies)

	var attributions []Attribution
	var w []warnings.StaticWarning
	invalid := func(id, format string, a ...any) {
		w = append(w, warnings.NewStaticWarning(csv, warnings.AttributionInvalid{AttributionID: id, Reason: fmt.Sprintf(format, a...)}))
	}
	seen := map[string]bool{}
rows:
	for csv.NextRow() {
		a := Attribution{
			Id:               idColumn.Read(),
			OrganizationName: organizationNameColumn.Read(),
			Url:              urlColumn.Read(),
			Email:            emailColumn.Read(),
			Phone:            phoneColumn.Read(),
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			invalid(a.Id, "missing values %s", missingKeys)
			continue
		}
		for _, role := range []struct {
			column string
			raw    string
			value  *bool
		}{
			{"is_producer", isProducerColumn.Read(), &a.IsProducer},
			{"is_operator", isOperatorColumn.Read(), &a.IsOperator},
			{"is_authority", isAuthorityColumn.Read(), &a.IsAuthority},
		} {
			v, ok := parseRole(role.raw)
			if !ok {
				invalid(a.Id, "invalid %s %q", role.column, role.raw)
				continue rows
			}
			*role.value = v
		}
		agencyID, routeID, tripID := agencyIDColumn.Read(), routeIDColumn.Read(), tripIDColumn.Read()
		scopes := 0
		for _, id := range []string{agencyID, routeID, tripID} {
			if id != "" {
				scopes++
			}
		}
		if scopes > 1 {
			invalid(a.Id, "more than one of agency_id, route_id and trip_id is set")
			continue
		}
		switch {
		case agencyID != "":
			if a.Agency = agencyIdToAgency[agencyID]; a.Agency == nil {
				invalid(a.Id, "unknown agency_id %q", agencyID)
				continue
			}
		case routeID != "":
			if a.Route = routeIdToRoute[routeID]; a.Route == nil {
				invalid(a.Id, "unknown route_id %q", routeID)
				continue
			}
		case tripID != "":
			if a.Trip = tripIdToTrip[tripID]; a.Trip == nil {
				invalid(a.Id, "unknown trip_id %q", tripID)
				continue
			}
		}
		if a.Id != "" && seen[a.Id] {
			invalid(a.Id, "duplicate attribution_id")
			continue
		}
		// GTFS recommends, but does not require, at least one role.
		if !a.IsProducer && !a.IsOperator && !a.IsAuthority {
			w = append(w, warnings.NewStaticWarning(csv, warnings.AttributionWithoutRole{AttributionID: a.Id}))
		}
		seen[a.Id] = true
		attributions = append(attributions, a)
	}
	return attributions, w
}

// parseRole reads an is_producer, is_operator or is_authority value, ignoring
// surrounding whitespace. Empty means 0. ok is false for any other value.
func parseRole(s string) (v, ok bool) {
	switch strings.TrimSpace(s) {
	case "", "0":
		return false, true
	case "1":
		return true, true
	default:
		return false, false
	}
}
