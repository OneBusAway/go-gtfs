package gtfs

import (
	"fmt"
	"testing"

	"github.com/OneBusAway/go-gtfs/warnings"
	"github.com/google/go-cmp/cmp"
)

// parseFaresV1Feed parses the default feed (agency a, route route_id) with
// agency.txt replaced when agencies is given, plus the fare files if given.
func parseFaresV1Feed(t *testing.T, agencies, attributes, rules []string) *Static {
	t.Helper()
	b := newZipBuilderWithDefaults()
	if agencies != nil {
		b.add("agency.txt", agencies...)
		b.add("routes.txt", "route_id,agency_id,route_type", "route_id,a,3")
	}
	if attributes != nil {
		b.add("fare_attributes.txt", attributes...)
	}
	if rules != nil {
		b.add("fare_rules.txt", rules...)
	}
	static, err := ParseStatic(b.build(), ParseStaticOptions{})
	if err != nil {
		t.Fatalf("ParseStatic: %s", err)
	}
	return static
}

// summarizeFareAttributes renders each fare as one line.
func summarizeFareAttributes(fares []FareAttribute) []string {
	var s []string
	for _, f := range fares {
		agency := ""
		if f.Agency != nil {
			agency = f.Agency.Id
		}
		transfers, duration := "unlimited", "-"
		if f.Transfers != nil {
			transfers = fmt.Sprint(*f.Transfers)
		}
		if f.TransferDuration != nil {
			duration = fmt.Sprint(*f.TransferDuration)
		}
		s = append(s, fmt.Sprintf("%s %s %s transfers=%s duration=%s agency=%s", f.Id, f.Price, f.PaymentMethod, transfers, duration, agency))
	}
	return s
}

// summarizeFareRules renders each rule as one line.
func summarizeFareRules(rules []FareRule) []string {
	var s []string
	for _, r := range rules {
		s = append(s, fmt.Sprintf("fare=%s route=%s origin=%s destination=%s contains=%s",
			r.Fare.Id, routeID(r.Route), r.OriginZoneId, r.DestinationZoneId, r.ContainsZoneId))
	}
	return s
}

func TestParseFareAttributes(t *testing.T) {
	const header = "fare_id,price,currency_type,payment_method,transfers,agency_id,transfer_duration"
	twoAgencies := []string{"agency_id,agency_name,agency_url,agency_timezone", "a,A,u,UTC", "b,B,u,UTC"}
	for _, tc := range []struct {
		desc         string
		agencies     []string
		attributes   []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "no fare_attributes.txt",
		},
		{
			desc: "all fields",
			attributes: []string{
				header,
				"local,2.75,USD,0,,a,",
				"express,3.25,USD,1,0,a,5400",
				"pass,10,USD,1,2,a,0",
				"yen,210,JPY,0,1,a,",
			},
			want: []string{
				"local 2.75 USD ON_BOARD transfers=unlimited duration=- agency=a",
				"express 3.25 USD BEFORE_BOARDING transfers=0 duration=5400 agency=a",
				"pass 10 USD BEFORE_BOARDING transfers=2 duration=0 agency=a",
				"yen 210 JPY ON_BOARD transfers=1 duration=- agency=a",
			},
		},
		{
			desc:       "one agency and no agency_id column",
			attributes: []string{"fare_id,price,currency_type,payment_method,transfers", "f,1.50,EUR,0,"},
			want:       []string{"f 1.50 EUR ON_BOARD transfers=unlimited duration=- agency=a"},
		},
		{
			desc:         "several agencies and no agency_id",
			agencies:     twoAgencies,
			attributes:   []string{header, "f,1.50,EUR,0,,,"},
			want:         []string{"f 1.50 EUR ON_BOARD transfers=unlimited duration=- agency="},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeAgencyAmbiguous{FareID: "f"}},
		},
		{
			desc:       "several agencies with agency_id",
			agencies:   twoAgencies,
			attributes: []string{header, "f,1.50,EUR,0,,b,"},
			want:       []string{"f 1.50 EUR ON_BOARD transfers=unlimited duration=- agency=b"},
		},
		{
			desc:         "unknown agency_id",
			attributes:   []string{header, "f,1,USD,0,,nope,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: `unknown agency_id "nope"`}},
		},
		{
			desc:         "negative price",
			attributes:   []string{header, "f,-1,USD,0,,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: `invalid price "-1"`}},
		},
		{
			desc:         "blank currency_type",
			attributes:   []string{header, "f,1,  ,0,,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: "missing values [currency_type]"}},
		},
		{
			desc:         "non-numeric price",
			attributes:   []string{header, "f,free,USD,0,,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: `invalid price "free"`}},
		},
		{
			desc:         "invalid payment_method",
			attributes:   []string{header, "f,1,USD,2,,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: `invalid payment_method "2"`}},
		},
		{
			desc:         "invalid transfers",
			attributes:   []string{header, "f,1,USD,0,3,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: `invalid transfers "3"`}},
		},
		{
			desc:         "missing required values",
			attributes:   []string{header, "f,,,,,a,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: "missing values [price currency_type payment_method]"}},
		},
		{
			desc:         "invalid transfer_duration is dropped",
			attributes:   []string{header, "f,1,USD,0,,a,-60"},
			want:         []string{"f 1 USD ON_BOARD transfers=unlimited duration=- agency=a"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalidValue{FareID: "f", Column: "transfer_duration", Value: "-60"}},
		},
		{
			desc:         "duplicate fare_id keeps the first",
			attributes:   []string{header, "f,1,USD,0,,a,", "f,2,USD,0,,a,"},
			want:         []string{"f 1 USD ON_BOARD transfers=unlimited duration=- agency=a"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareAttributeInvalid{FareID: "f", Reason: "duplicate fare_id"}},
		},
		{
			desc:         "missing transfers column",
			attributes:   []string{"fare_id,price,currency_type,payment_method", "f,1,USD,0"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"transfers"}}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV1Feed(t, tc.agencies, tc.attributes, nil)
			if diff := cmp.Diff(summarizeFareAttributes(static.FareAttributes), tc.want); diff != "" {
				t.Errorf("FareAttributes mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

func TestParseFareRules(t *testing.T) {
	attributes := []string{"fare_id,price,currency_type,payment_method,transfers", "f,1,USD,0,", "g,2,USD,0,"}
	const header = "fare_id,route_id,origin_id,destination_id,contains_id"
	for _, tc := range []struct {
		desc         string
		rules        []string
		want         []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc: "no fare_rules.txt",
		},
		{
			desc:  "route only",
			rules: []string{header, "f,route_id,,,"},
			want:  []string{"fare=f route=route_id origin= destination= contains="},
		},
		{
			desc:  "origin and destination zones",
			rules: []string{header, "g,,zone1,zone2,"},
			want:  []string{"fare=g route= origin=zone1 destination=zone2 contains="},
		},
		{
			desc:  "contains zones",
			rules: []string{header, "f,,,,zone1", "f,,,,zone2"},
			want:  []string{"fare=f route= origin= destination= contains=zone1", "fare=f route= origin= destination= contains=zone2"},
		},
		{
			desc:  "only fare_id",
			rules: []string{"fare_id", "f"},
			want:  []string{"fare=f route= origin= destination= contains="},
		},
		{
			desc:         "unknown fare_id",
			rules:        []string{header, "nope,route_id,,,", "f,route_id,,,"},
			want:         []string{"fare=f route=route_id origin= destination= contains="},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareRuleInvalid{FareID: "nope", Reason: "unknown fare_id"}},
		},
		{
			desc:  "unknown route_id",
			rules: []string{header, "f,nope,,,"},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FareRuleInvalid{FareID: "f", Reason: `unknown route_id "nope"`},
				warnings.FareAttributeInvalid{FareID: "f", Reason: "every fare_rules.txt row for it was skipped"},
			},
		},
		{
			desc:         "missing fare_id",
			rules:        []string{header, ",route_id,,,"},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareRuleInvalid{Reason: "missing values [fare_id]"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV1Feed(t, nil, attributes, tc.rules)
			if diff := cmp.Diff(summarizeFareRules(static.FareRules), tc.want); diff != "" {
				t.Errorf("FareRules mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
		})
	}
}

// A partly read fare_rules.txt could leave a fare with no rules, which GTFS
// reads as applying to every trip, so an unreadable fare file drops both.
func TestParseFaresV1_UnreadableFileDropsBoth(t *testing.T) {
	attributes := []string{"fare_id,price,currency_type,payment_method,transfers", "f,1,USD,0,"}
	for _, tc := range []struct {
		desc              string
		attributes, rules []string
	}{
		{"fare_rules.txt", attributes, []string{"fare_id,route_id", "f,route_id", "broken"}},
		{"fare_attributes.txt", append(attributes, "broken"), []string{"fare_id,route_id", "f,route_id"}},
		{"fare_rules.txt header", attributes, []string{`"fare_id`}},
		{"fare_attributes.txt header", []string{`"fare_id`}, []string{"fare_id,route_id", "f,route_id"}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV1Feed(t, nil, tc.attributes, tc.rules)
			if len(static.FareAttributes) != 0 || len(static.FareRules) != 0 {
				t.Errorf("got %d fares and %d rules, want none", len(static.FareAttributes), len(static.FareRules))
			}
			var unreadable int
			for _, w := range static.Warnings {
				if _, ok := w.Kind.(warnings.FileUnreadable); ok {
					unreadable++
				}
			}
			if unreadable != 1 {
				t.Errorf("got warnings %v, want one FileUnreadable", warningKinds(static.Warnings))
			}
		})
	}
}

func TestParseCurrencyAmount(t *testing.T) {
	for _, tc := range []struct {
		in       string
		want     CurrencyAmount
		wantOK   bool
		wantText string
	}{
		{"2.75", CurrencyAmount{Units: 275, Scale: 2, Currency: "USD"}, true, "2.75 USD"},
		{"0.1", CurrencyAmount{Units: 1, Scale: 1, Currency: "USD"}, true, "0.1 USD"},
		{"10", CurrencyAmount{Units: 10, Scale: 0, Currency: "USD"}, true, "10 USD"},
		{"0", CurrencyAmount{Currency: "USD"}, true, "0 USD"},
		{"2.50", CurrencyAmount{Units: 250, Scale: 2, Currency: "USD"}, true, "2.50 USD"},
		{" 3.5 ", CurrencyAmount{Units: 35, Scale: 1, Currency: "USD"}, true, "3.5 USD"},
		{"-1.25", CurrencyAmount{Units: -125, Scale: 2, Currency: "USD"}, true, "-1.25 USD"},
		{"-0.05", CurrencyAmount{Units: -5, Scale: 2, Currency: "USD"}, true, "-0.05 USD"},
		{"", CurrencyAmount{}, false, ""},
		{"1.", CurrencyAmount{}, false, ""},
		{".5", CurrencyAmount{}, false, ""},
		{"1e3", CurrencyAmount{}, false, ""},
		{"NaN", CurrencyAmount{}, false, ""},
		{"1,50", CurrencyAmount{}, false, ""},
		{"99999999999999999999", CurrencyAmount{}, false, ""},
	} {
		got, ok := parseCurrencyAmount(tc.in, "USD")
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("parseCurrencyAmount(%q) = %+v, %t, want %+v, %t", tc.in, got, ok, tc.want, tc.wantOK)
		}
		if ok && got.String() != tc.wantText {
			t.Errorf("parseCurrencyAmount(%q).String() = %q, want %q", tc.in, got.String(), tc.wantText)
		}
	}
}

func TestCurrencyAmountFloat64(t *testing.T) {
	if got := (CurrencyAmount{Units: 275, Scale: 2}).Float64(); got != 2.75 {
		t.Errorf("Float64() = %v, want 2.75", got)
	}
	if got := (CurrencyAmount{Units: -5, Scale: 2}).Float64(); got != -0.05 {
		t.Errorf("Float64() = %v, want -0.05", got)
	}
}

// GTFS reads a fare with no rules as applying to every trip, so a fare whose
// rules were all skipped must not be kept as if it had none.
func TestParseFareRules_FareLeftWithoutRules(t *testing.T) {
	attributes := []string{"fare_id,price,currency_type,payment_method,transfers", "f,1,USD,0,", "g,2,USD,0,", "h,3,USD,0,"}
	for _, tc := range []struct {
		desc         string
		rules        []string
		wantFares    []string
		wantRules    []string
		wantWarnings []warnings.StaticWarningKind
	}{
		{
			desc:      "only rule skipped drops the fare",
			rules:     []string{"fare_id,route_id", "f,nope", "g,route_id", "h,route_id"},
			wantFares: []string{"g", "h"},
			wantRules: []string{"fare=g route=route_id origin= destination= contains=", "fare=h route=route_id origin= destination= contains="},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FareRuleInvalid{FareID: "f", Reason: `unknown route_id "nope"`},
				warnings.FareAttributeInvalid{FareID: "f", Reason: "every fare_rules.txt row for it was skipped"},
			},
		},
		{
			desc:         "one of two rules skipped keeps the fare",
			rules:        []string{"fare_id,route_id", "f,route_id", "f,nope"},
			wantFares:    []string{"f", "g", "h"},
			wantRules:    []string{"fare=f route=route_id origin= destination= contains="},
			wantWarnings: []warnings.StaticWarningKind{warnings.FareRuleInvalid{FareID: "f", Reason: `unknown route_id "nope"`}},
		},
		{
			// A trip must pass through every contains_id zone, so losing one
			// would widen the fare.
			desc:      "skipped contains_id row drops the fare",
			rules:     []string{"fare_id,route_id,contains_id", "f,,zone1", "f,nope,zone2", "g,route_id,"},
			wantFares: []string{"g", "h"},
			wantRules: []string{"fare=g route=route_id origin= destination= contains="},
			wantWarnings: []warnings.StaticWarningKind{
				warnings.FareRuleInvalid{FareID: "f", Reason: `unknown route_id "nope"`},
				warnings.FareAttributeInvalid{FareID: "f", Reason: "a fare_rules.txt row with a contains_id for it was skipped"},
			},
		},
		{
			desc:         "fare_rules.txt without fare_id drops every fare",
			rules:        []string{"route_id", "route_id"},
			wantWarnings: []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"fare_id"}}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			static := parseFaresV1Feed(t, nil, attributes, tc.rules)
			var fares []string
			for _, f := range static.FareAttributes {
				fares = append(fares, f.Id)
			}
			if diff := cmp.Diff(fares, tc.wantFares); diff != "" {
				t.Errorf("fares mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(summarizeFareRules(static.FareRules), tc.wantRules); diff != "" {
				t.Errorf("FareRules mismatch (-got +want):\n%s", diff)
			}
			if diff := cmp.Diff(warningKinds(static.Warnings), tc.wantWarnings); diff != "" {
				t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
			}
			fareByID := faresByID(static.FareAttributes)
			for _, r := range static.FareRules {
				if r.Fare != fareByID[r.Fare.Id] {
					t.Errorf("rule for %s does not point into Static.FareAttributes", r.Fare.Id)
				}
			}
		})
	}
}

// Without usable fares, fare_rules.txt rows would only report unknown fares.
func TestParseFareRules_SkippedWhenFareAttributesUnusable(t *testing.T) {
	static := parseFaresV1Feed(t, nil,
		[]string{"fare_id,price,currency_type,payment_method", "f,1,USD,0"},
		[]string{"fare_id,route_id", "f,route_id", "g,route_id"})
	want := []warnings.StaticWarningKind{warnings.MissingColumns{Columns: []string{"transfers"}}}
	if diff := cmp.Diff(warningKinds(static.Warnings), want); diff != "" {
		t.Errorf("Warnings mismatch (-got +want):\n%s", diff)
	}
}
