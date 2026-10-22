package gtfs

import (
	"time"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// FeedInfo describes the dataset itself: who publishes it, its language and
// the period it covers. It comes from feed_info.txt.
type FeedInfo struct {
	PublisherName string
	PublisherUrl  string
	// Language is the dataset's language code, or "mul" if it has text in
	// more than one language.
	Language        string
	DefaultLanguage string
	// StartDate and EndDate bound the period of complete and reliable
	// schedule data, in the first agency's timezone. Nil when not given.
	StartDate    *time.Time
	EndDate      *time.Time
	Version      string
	ContactEmail string
	ContactUrl   string
}

// parseFeedInfo reads the first row of feed_info.txt. The file has one row;
// any more add a warning and are ignored. A row with empty required values
// is still kept, since the rest of it describes the feed.
func parseFeedInfo(csv *csv.File, timezone *time.Location) (*FeedInfo, []warnings.StaticWarning) {
	publisherNameColumn := csv.RequiredColumn("feed_publisher_name")
	publisherUrlColumn := csv.RequiredColumn("feed_publisher_url")
	languageColumn := csv.RequiredColumn("feed_lang")
	defaultLanguageColumn := csv.OptionalColumn("default_lang")
	startDateColumn := csv.OptionalColumn("feed_start_date")
	endDateColumn := csv.OptionalColumn("feed_end_date")
	versionColumn := csv.OptionalColumn("feed_version")
	contactEmailColumn := csv.OptionalColumn("feed_contact_email")
	contactUrlColumn := csv.OptionalColumn("feed_contact_url")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var info *FeedInfo
	var w []warnings.StaticWarning
	parseDate := func(column, raw string) *time.Time {
		if raw == "" {
			return nil
		}
		date, err := parseTime(raw, timezone)
		if err != nil {
			w = append(w, warnings.NewStaticWarning(csv, warnings.FeedInfoInvalidDate{Column: column, Value: raw}))
			return nil
		}
		return &date
	}
	for csv.NextRow() {
		if info != nil {
			w = append(w, warnings.NewStaticWarning(csv, warnings.FeedInfoExtraRow{}))
			continue
		}
		info = &FeedInfo{
			PublisherName:   publisherNameColumn.Read(),
			PublisherUrl:    publisherUrlColumn.Read(),
			Language:        languageColumn.Read(),
			DefaultLanguage: defaultLanguageColumn.Read(),
			StartDate:       parseDate("feed_start_date", startDateColumn.Read()),
			EndDate:         parseDate("feed_end_date", endDateColumn.Read()),
			Version:         versionColumn.Read(),
			ContactEmail:    contactEmailColumn.Read(),
			ContactUrl:      contactUrlColumn.Read(),
		}
		if missingKeys := csv.MissingRowKeys(); len(missingKeys) > 0 {
			w = append(w, warnings.NewStaticWarning(csv, warnings.FeedInfoMissingValues{Columns: missingKeys}))
		}
	}
	return info, w
}
