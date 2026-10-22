package gtfs

import (
	"testing"
	"time"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseFeedInfoFeed parses the default feed, with its agency in
// America/Los_Angeles, plus feed_info.txt if lines are given.
func parseFeedInfoFeed(t *testing.T, lines ...string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add(
		"agency.txt",
		"agency_id,agency_name,agency_url,agency_timezone\na,b,c,America/Los_Angeles",
	)
	if lines != nil {
		b.add("feed_info.txt", lines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

func TestParseFeedInfo(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	const fullHeader = "feed_publisher_name,feed_publisher_url,feed_lang,default_lang,feed_start_date,feed_end_date,feed_version,feed_contact_email,feed_contact_url"
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         *FeedInfo
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:  "no feed_info.txt",
			lines: nil,
			want:  nil,
		},
		{
			desc: "all fields",
			lines: []string{
				fullHeader,
				`TriMet,https://trimet.org/,en,en,20261011,20270227,"Fall 2026, version D",dev@example.com,https://example.com/contact`,
			},
			want: &FeedInfo{
				PublisherName:   "TriMet",
				PublisherUrl:    "https://trimet.org/",
				Language:        "en",
				DefaultLanguage: "en",
				StartDate:       ptr(time.Date(2026, 10, 11, 0, 0, 0, 0, la)),
				EndDate:         ptr(time.Date(2027, 2, 27, 0, 0, 0, 0, la)),
				Version:         "Fall 2026, version D",
				ContactEmail:    "dev@example.com",
				ContactUrl:      "https://example.com/contact",
			},
		},
		{
			desc: "only required fields",
			lines: []string{
				"feed_publisher_name,feed_publisher_url,feed_lang",
				"Metro,https://example.com,mul",
			},
			want: &FeedInfo{PublisherName: "Metro", PublisherUrl: "https://example.com", Language: "mul"},
		},
		{
			desc: "empty dates are nil",
			lines: []string{
				fullHeader,
				"Metro,https://example.com,en,,,,,,",
			},
			want: &FeedInfo{PublisherName: "Metro", PublisherUrl: "https://example.com", Language: "en"},
		},
		{
			desc: "invalid dates are nil with a warning",
			lines: []string{
				fullHeader,
				"Metro,https://example.com,en,,2026-10-11,20261301,,,",
			},
			want: &FeedInfo{PublisherName: "Metro", PublisherUrl: "https://example.com", Language: "en"},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FeedInfoInvalidDate{Column: "feed_start_date", Value: "2026-10-11"},
				warnings.FeedInfoInvalidDate{Column: "feed_end_date", Value: "20261301"},
			},
		},
		{
			desc: "only the first row is read",
			lines: []string{
				"feed_publisher_name,feed_publisher_url,feed_lang",
				"First,https://first.example.com,en",
				"Second,https://second.example.com,fr",
				"Third,https://third.example.com,de",
			},
			want: &FeedInfo{PublisherName: "First", PublisherUrl: "https://first.example.com", Language: "en"},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FeedInfoExtraRow{},
				warnings.FeedInfoExtraRow{},
			},
		},
		{
			desc: "empty required values are kept with a warning",
			lines: []string{
				"feed_publisher_name,feed_publisher_url,feed_lang",
				"Metro,,",
			},
			want: &FeedInfo{PublisherName: "Metro"},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FeedInfoMissingValues{Columns: []string{"feed_publisher_url", "feed_lang"}},
			},
		},
		{
			desc: "missing required column",
			lines: []string{
				"feed_publisher_name,feed_publisher_url",
				"Metro,https://example.com",
			},
			want: nil,
			wantWarnings: []warnings.StaticWarningKind{
				warnings.MissingColumns{Columns: []string{"feed_lang"}},
			},
		},
		{
			desc:  "header without rows",
			lines: []string{"feed_publisher_name,feed_publisher_url,feed_lang"},
			want:  nil,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFeedInfoFeed(t, tc.lines...)
			if diff := cmp.Diff(static.FeedInfo, tc.want); diff != "" {
				t.Errorf("FeedInfo mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseFeedInfo_EmptyFileIsAbsent(t *testing.T) {
	static := parseFeedInfoFeed(t, "")
	if static.FeedInfo != nil {
		t.Errorf("FeedInfo = %+v, want nil", static.FeedInfo)
	}
	if len(static.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", static.Warnings)
	}
}

func TestParseFeedInfo_ExtraRowWarningPointsAtRow(t *testing.T) {
	static := parseFeedInfoFeed(t,
		"feed_publisher_name,feed_publisher_url,feed_lang",
		"First,https://first.example.com,en",
		"Second,https://second.example.com,fr",
	)
	want := []warnings.StaticWarning{{
		Kind:          warnings.FeedInfoExtraRow{},
		File:          constants.FeedInfoFile,
		RowNumber:     2,
		RowContent:    []string{"Second", "https://second.example.com", "fr"},
		HeaderContent: []string{"feed_publisher_name", "feed_publisher_url", "feed_lang"},
	}}
	if diff := cmp.Diff(static.Warnings, want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}
