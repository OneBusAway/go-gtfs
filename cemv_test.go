package gtfs

import "testing"

func TestCemvSupportString(t *testing.T) {
	for _, tc := range []struct {
		in   CemvSupport
		want string
	}{
		{CemvSupport_NotSpecified, "NOT_SPECIFIED"},
		{CemvSupport_Supported, "SUPPORTED"},
		{CemvSupport_NotSupported, "NOT_SUPPORTED"},
		{CemvSupport(99), "UNKNOWN"},
	} {
		if got := tc.in.String(); got != tc.want {
			t.Errorf("CemvSupport(%d).String() = %q, want %q", int32(tc.in), got, tc.want)
		}
	}
}

func TestRoute_EffectiveCemvSupport(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		agency CemvSupport
		route  CemvSupport
		want   CemvSupport
	}{
		{"route overrides agency", CemvSupport_Supported, CemvSupport_NotSupported, CemvSupport_NotSupported},
		{"unspecified route inherits agency", CemvSupport_Supported, CemvSupport_NotSpecified, CemvSupport_Supported},
		{"route supported, agency not", CemvSupport_NotSupported, CemvSupport_Supported, CemvSupport_Supported},
		{"both unspecified", CemvSupport_NotSpecified, CemvSupport_NotSpecified, CemvSupport_NotSpecified},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			r := &Route{CemvSupport: tc.route, Agency: &Agency{CemvSupport: tc.agency}}
			if got := r.EffectiveCemvSupport(); got != tc.want {
				t.Errorf("EffectiveCemvSupport() = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("nil agency", func(t *testing.T) {
		if got := (&Route{}).EffectiveCemvSupport(); got != CemvSupport_NotSpecified {
			t.Errorf("EffectiveCemvSupport() = %v, want NotSpecified", got)
		}
	})
}

func TestParseStatic_CemvSupportAgency(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		header string
		row    string
		want   CemvSupport
	}{
		{"column absent", "agency_id,agency_name,agency_url,agency_timezone", "a,b,c,d", CemvSupport_NotSpecified},
		{"empty", "agency_id,agency_name,agency_url,agency_timezone,cemv_support", "a,b,c,d,", CemvSupport_NotSpecified},
		{"0", "agency_id,agency_name,agency_url,agency_timezone,cemv_support", "a,b,c,d,0", CemvSupport_NotSpecified},
		{"1", "agency_id,agency_name,agency_url,agency_timezone,cemv_support", "a,b,c,d,1", CemvSupport_Supported},
		{"2", "agency_id,agency_name,agency_url,agency_timezone,cemv_support", "a,b,c,d,2", CemvSupport_NotSupported},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			content := newZipBuilderWithDefaults().add("agency.txt", tc.header, tc.row).build()
			static, err := ParseStatic(content, ParseStaticOptions{StopTimes: StopTimesNone})
			if err != nil {
				t.Fatalf("ParseStatic() got error %v, want nil", err)
			}
			if len(static.Agencies) != 1 {
				t.Fatalf("got %d agencies, want 1", len(static.Agencies))
			}
			if got := static.Agencies[0].CemvSupport; got != tc.want {
				t.Errorf("Agency.CemvSupport = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseStatic_CemvSupportRoutes(t *testing.T) {
	content := newZipBuilderWithDefaults().
		add("agency.txt",
			"agency_id,agency_name,agency_url,agency_timezone,cemv_support",
			"a,b,c,d,1",
		).
		add("routes.txt",
			"route_id,route_type,cemv_support",
			"r_empty,3,",
			"r_zero,3,0",
			"r_one,3,1",
			"r_two,3,2",
		).build()
	static, err := ParseStatic(content, ParseStaticOptions{StopTimes: StopTimesNone})
	if err != nil {
		t.Fatalf("ParseStatic() got error %v, want nil", err)
	}

	// The agency has cemv_support=1, so an empty or 0 route value inherits it
	// and only an explicit 2 on the route overrides it.
	want := map[string]struct{ own, effective CemvSupport }{
		"r_empty": {CemvSupport_NotSpecified, CemvSupport_Supported},
		"r_zero":  {CemvSupport_NotSpecified, CemvSupport_Supported},
		"r_one":   {CemvSupport_Supported, CemvSupport_Supported},
		"r_two":   {CemvSupport_NotSupported, CemvSupport_NotSupported},
	}
	if len(static.Routes) != len(want) {
		t.Fatalf("got %d routes, want %d", len(static.Routes), len(want))
	}
	for i := range static.Routes {
		r := &static.Routes[i]
		w, ok := want[r.Id]
		if !ok {
			t.Errorf("unexpected route %q", r.Id)
			continue
		}
		if r.CemvSupport != w.own {
			t.Errorf("route %s: CemvSupport = %v, want %v", r.Id, r.CemvSupport, w.own)
		}
		if got := r.EffectiveCemvSupport(); got != w.effective {
			t.Errorf("route %s: EffectiveCemvSupport() = %v, want %v", r.Id, got, w.effective)
		}
	}
}
