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

	agencyIdToAgency := make(map[string]*Agency, len(agencies))
	for i := range agencies {
		agencyIdToAgency[agencies[i].Id] = &agencies[i]
	}

	var attributions []Attribution
	var w []warnings.StaticWarning
	seen := map[string]bool{}
	for csv.NextRow() {
		a := Attribution{
			Id:               idColumn.Read(),
			OrganizationName: organizationNameColumn.Read(),
			Url:              urlColumn.Read(),
			Email:            emailColumn.Read(),
			Phone:            phoneColumn.Read(),
		}
		reason := func() string {
			if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
				return fmt.Sprintf("missing values %s", missingKeys)
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
				switch strings.TrimSpace(role.raw) {
				case "", "0":
				case "1":
					*role.value = true
				default:
					return fmt.Sprintf("invalid %s %q", role.column, role.raw)
				}
			}
			agencyID, routeID, tripID := agencyIDColumn.Read(), routeIDColumn.Read(), tripIDColumn.Read()
			switch {
			case (agencyID != "" && routeID != "") || (agencyID != "" && tripID != "") || (routeID != "" && tripID != ""):
				return "more than one of agency_id, route_id and trip_id is set"
			case agencyID != "":
				if a.Agency = agencyIdToAgency[agencyID]; a.Agency == nil {
					return fmt.Sprintf("unknown agency_id %q", agencyID)
				}
			case routeID != "":
				if a.Route = routeIdToRoute[routeID]; a.Route == nil {
					return fmt.Sprintf("unknown route_id %q", routeID)
				}
			case tripID != "":
				if a.Trip = tripIdToTrip[tripID]; a.Trip == nil {
					return fmt.Sprintf("unknown trip_id %q", tripID)
				}
			}
			if a.Id != "" && seen[a.Id] {
				return "duplicate attribution_id"
			}
			return ""
		}()
		if reason != "" {
			w = append(w, warnings.NewStaticWarning(csv, warnings.AttributionInvalid{AttributionID: a.Id, Reason: reason}))
			continue
		}
		seen[a.Id] = true
		attributions = append(attributions, a)
	}
	return attributions, w
}
