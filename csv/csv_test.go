package csv

import (
	"io"
	"strings"
	"testing"

	"github.com/OneBusAway/go-gtfs/constants"
)

func TestNew_MatchesHeaderNamesWithSurroundingSpaces(t *testing.T) {
	content := "trip_id, stop_id ,stop_sequence\nt1,s1,1\n"
	f, err := New(constants.StaticFile("stop_times.txt"), io.NopCloser(strings.NewReader(content)))
	if err != nil {
		t.Fatal(err)
	}
	stopID := f.RequiredColumn("stop_id")
	if missing := f.MissingRequiredColumns(); len(missing) > 0 {
		t.Fatalf("missing required columns %v", missing)
	}
	if !f.NextRow() {
		t.Fatal("expected a row")
	}
	if got := stopID.Read(); got != "s1" {
		t.Errorf("stop_id = %q, want %q", got, "s1")
	}
}
