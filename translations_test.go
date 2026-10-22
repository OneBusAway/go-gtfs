package gtfs

import (
	"testing"
	"unsafe"

	"github.com/OneBusAway/go-gtfs/constants"
	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

const translationsHeader = "table_name,field_name,language,translation,record_id,record_sub_id,field_value"

// parseTranslationsFeed parses the default feed plus feed_info.txt and, if
// lines are given, translations.txt.
func parseTranslationsFeed(t *testing.T, lines ...string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults().add("feed_info.txt", "feed_publisher_name,feed_publisher_url,feed_lang", "Metro,https://example.com,fi")
	if lines != nil {
		b.add("translations.txt", lines...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

func TestParseTranslations(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		lines        []string
		want         []Translation
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "no translations.txt",
		},
		{
			desc:  "by record_id",
			lines: []string{translationsHeader, "stops,stop_name,sv,Centralstationen,S1,,"},
			want:  []Translation{{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "Centralstationen", RecordId: "S1"}},
		},
		{
			desc:  "by record_id and record_sub_id",
			lines: []string{translationsHeader, "stop_times,stop_headsign,sv,Kivistö st.,T1,3,"},
			want:  []Translation{{TableName: "stop_times", FieldName: "stop_headsign", Language: "sv", Translation: "Kivistö st.", RecordId: "T1", RecordSubId: "3"}},
		},
		{
			desc:  "by field_value",
			lines: []string{translationsHeader, `"stop_times","stop_headsign","sv","Sökö udd via Esboviken (M)",,,"Soukanniemi via Espoonlahti (M)"`},
			want: []Translation{{
				TableName: "stop_times", FieldName: "stop_headsign", Language: "sv",
				Translation: "Sökö udd via Esboviken (M)", FieldValue: "Soukanniemi via Espoonlahti (M)",
			}},
		},
		{
			desc:  "feed_info",
			lines: []string{translationsHeader, "feed_info,feed_publisher_name,sv,Metro SV,,,"},
			want:  []Translation{{TableName: "feed_info", FieldName: "feed_publisher_name", Language: "sv", Translation: "Metro SV"}},
		},
		{
			desc:  "a table the parser does not read",
			lines: []string{translationsHeader, "fare_products,fare_product_name,sv,Biljett,F1,,"},
			want:  []Translation{{TableName: "fare_products", FieldName: "fare_product_name", Language: "sv", Translation: "Biljett", RecordId: "F1"}},
		},
		{
			desc:  "only the required columns",
			lines: []string{"table_name,field_name,language,translation,field_value", "routes,route_long_name,sv,Ring,Kehä"},
			want:  []Translation{{TableName: "routes", FieldName: "route_long_name", Language: "sv", Translation: "Ring", FieldValue: "Kehä"}},
		},
		{
			desc:         "missing translation",
			lines:        []string{translationsHeader, "stops,stop_name,sv,,S1,,", "stops,stop_name,sv,Kyrkan,S2,,"},
			want:         []Translation{{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "Kyrkan", RecordId: "S2"}},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "missing values [translation]"}},
		},
		{
			desc:         "missing table_name and language",
			lines:        []string{translationsHeader, ",stop_name,,Kyrkan,S2,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "missing values [table_name language]"}},
		},
		{
			desc:         "record_id and field_value",
			lines:        []string{translationsHeader, "stops,stop_name,sv,Kyrkan,S2,,Kirkko"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "record_id and field_value are both set"}},
		},
		{
			desc:         "neither record_id nor field_value",
			lines:        []string{translationsHeader, "stops,stop_name,sv,Kyrkan,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "neither record_id nor field_value is set"}},
		},
		{
			desc:         "record_sub_id with field_value",
			lines:        []string{translationsHeader, "stop_times,stop_headsign,sv,Kyrkan,,3,Kirkko"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "record_sub_id is set with field_value"}},
		},
		{
			desc:         "stop_times record_id without record_sub_id",
			lines:        []string{translationsHeader, "stop_times,stop_headsign,sv,Kyrkan,T1,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "stop_times record_id needs a record_sub_id"}},
		},
		{
			desc:         "feed_info with a record selector",
			lines:        []string{translationsHeader, "feed_info,feed_publisher_name,sv,Metro SV,,,Metro"},
			wantWarnings: []warnings.StaticWarningKind{warnings.TranslationInvalid{Reason: "feed_info rows cannot set record_id, record_sub_id or field_value"}},
		},
		{
			desc:         "missing required column",
			lines:        []string{"table_name,field_name,language,record_id", "stops,stop_name,sv,S1"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"translation"}}},
		},
		{
			desc:         "unreadable translations.txt keeps the rows read before the error",
			lines:        []string{translationsHeader, "stops,stop_name,sv,Kyrkan,S2,,", "broken"},
			want:         []Translation{{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "Kyrkan", RecordId: "S2"}},
			wantWarnings: []warnings.StaticWarningKind{warnings.FileUnreadable{Reason: "record on line 3: wrong number of fields"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseTranslationsFeed(t, tc.lines...)
			if diff := cmp.Diff(static.Translations, tc.want); diff != "" {
				t.Errorf("Translations mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseTranslations_WithoutFeedInfo(t *testing.T) {
	feed := newZipBuilderWithDefaults().add("translations.txt", translationsHeader, "stops,stop_name,sv,Kyrkan,S2,,").build()
	static, err := ParseStatic(feed, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	if len(static.Translations) != 1 {
		t.Errorf("got %d translations, want 1", len(static.Translations))
	}
	want := []warnings.StaticWarning{warnings.NewFileWarning(constants.TranslationsFile, warnings.TranslationsWithoutFeedInfo{})}
	if diff := cmp.Diff(static.Warnings, want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}

func TestTranslator(t *testing.T) {
	translator := NewTranslator([]Translation{
		{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "Kyrkan (by value)", FieldValue: "Kirkko"},
		{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "Kyrkan (S1)", RecordId: "S1"},
		{TableName: "stop_times", FieldName: "stop_headsign", Language: "sv", Translation: "Centrum", RecordId: "T1", RecordSubId: "3"},
		{TableName: "feed_info", FieldName: "feed_publisher_name", Language: "sv", Translation: "Metro SV"},
		{TableName: "stops", FieldName: "stop_name", Language: "sv", Translation: "duplicate", RecordId: "S1"},
	})
	for _, tc := range []struct {
		desc                         string
		table, field, language       string
		recordID, recordSubID, value string
		want                         string
		wantOK                       bool
	}{
		{"record_id wins over field_value", "stops", "stop_name", "sv", "S1", "", "Kirkko", "Kyrkan (S1)", true},
		{"field_value applies to other records", "stops", "stop_name", "sv", "S2", "", "Kirkko", "Kyrkan (by value)", true},
		{"field_value must match exactly", "stops", "stop_name", "sv", "S2", "", "Kirkko 2", "", false},
		{"other language", "stops", "stop_name", "en", "S1", "", "Kirkko", "", false},
		{"other field", "stops", "stop_desc", "sv", "S1", "", "Kirkko", "", false},
		{"record_sub_id", "stop_times", "stop_headsign", "sv", "T1", "3", "Keskusta", "Centrum", true},
		{"other record_sub_id", "stop_times", "stop_headsign", "sv", "T1", "4", "Keskusta", "", false},
		{"feed_info", "feed_info", "feed_publisher_name", "sv", "", "", "Metro", "Metro SV", true},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, ok := translator.Translate(tc.table, tc.field, tc.language, tc.recordID, tc.recordSubID, tc.value)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("Translate() = %q, %t, want %q, %t", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// Large feeds repeat translation values across many rows, for example the
// same headsign for every trip that uses it, so equal values share storage.
func TestParseTranslations_RepeatedValuesShareStorage(t *testing.T) {
	static := parseTranslationsFeed(t, translationsHeader,
		"stop_times,stop_headsign,sv,Centrum,T1,3,",
		"stop_times,stop_headsign,sv,Centrum,T1,4,",
		"stop_times,stop_headsign,sv,Centrum,T2,3,",
	)
	a, b, c := static.Translations[0], static.Translations[1], static.Translations[2]
	if unsafe.StringData(a.Translation) != unsafe.StringData(b.Translation) {
		t.Error("equal Translation values do not share storage")
	}
	if unsafe.StringData(a.RecordId) != unsafe.StringData(b.RecordId) {
		t.Error("equal RecordId values do not share storage")
	}
	if unsafe.StringData(a.RecordSubId) != unsafe.StringData(c.RecordSubId) {
		t.Error("equal RecordSubId values do not share storage")
	}
}

func TestParseTranslations_FeedInfoWithoutRows(t *testing.T) {
	feed := newZipBuilderWithDefaults().
		add("feed_info.txt", "feed_publisher_name,feed_publisher_url,feed_lang").
		add("translations.txt", translationsHeader, "stops,stop_name,sv,Kyrkan,S2,,").
		build()
	static, err := ParseStatic(feed, ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	want := warnings.NewFileWarning(constants.TranslationsFile, warnings.TranslationsWithoutFeedInfo{})
	for _, w := range static.Warnings {
		if cmp.Equal(w, want) {
			return
		}
	}
	t.Errorf("no TranslationsWithoutFeedInfo warning in %v", static.Warnings)
}
