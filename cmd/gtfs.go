package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/OneBusAway/go-gtfs"
	"github.com/OneBusAway/go-gtfs/extensions/nyctalerts"
	"github.com/OneBusAway/go-gtfs/extensions/nycttrips"
	"github.com/OneBusAway/go-gtfs/journal"
	"github.com/fatih/color"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "GTFS parser",
		Usage: "parse GTFS static and realtime feeds",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "print additional data about each trip and vehicle",
			},
			&cli.StringFlag{
				Name:  "extension",
				Usage: "GTFS realtime extension to use: nycttrips, nyctalerts",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "static",
				Usage: "parse a GTFS static message",
				Action: func(*cli.Context) error {
					path := "google_transit.zip"
					b, err := os.ReadFile(path)
					if err != nil {
						return fmt.Errorf("failed to read file %s: %w", path, err)
					}
					static, err := gtfs.ParseStatic(b, gtfs.ParseStaticOptions{})
					if err != nil {
						return fmt.Errorf("failed to parse GTFS static data: %w", err)
					}
					fmt.Println("Num trips", len(static.Trips))
					return nil
				},
			},
			{
				Name:      "realtime",
				Usage:     "parse a GTFS realtime message",
				ArgsUsage: "path",
				Action: func(ctx *cli.Context) error {
					args := ctx.Args()
					if args.Len() == 0 {
						return fmt.Errorf("a path to the GTFS realtime message was not provided")
					}
					path := ctx.Args().First()
					b, err := os.ReadFile(path)
					if err != nil {
						return fmt.Errorf("failed to read file %s: %w", path, err)
					}
					opts := gtfs.ParseRealtimeOptions{}
					rawExtension := ctx.String("extension")
					if err := readGtfsRealtimeExtension(rawExtension, &opts); err != nil {
						return err
					}
					realtime, err := gtfs.ParseRealtime(b, &opts)
					if err != nil {
						return fmt.Errorf("failed to parse message: %w", err)
					}
					fmt.Printf("Created at %s\n", realtime.CreatedAt)
					fmt.Printf("%d trips:\n", len(realtime.Trips))
					for _, trip := range realtime.Trips {
						fmt.Printf("- %s\n", formatTrip(trip, 2, ctx.Bool("verbose")))
					}
					fmt.Printf("%d alerts:\n", len(realtime.Alerts))
					for _, alert := range realtime.Alerts {
						fmt.Printf("- %s\n", formatAlert(alert, 2))
					}
					fmt.Printf("%d vehicles:\n", len(realtime.Vehicles))
					for _, vehicle := range realtime.Vehicles {
						fmt.Printf("- %s\n", formatVehicle(vehicle, 2))
					}
					return nil
				},
			},
			{
				Name:  "journal",
				Usage: "build a journal from a series of GTFS realtime messages",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "output",
						Aliases: []string{"o"},
						Usage:   "directory to output the CSV files",
					},
				},
				ArgsUsage: "path",
				Action: func(ctx *cli.Context) error {
					args := ctx.Args()
					if args.Len() == 0 {
						return fmt.Errorf("a path to the GTFS realtime messages was not provided")
					}
					path := ctx.Args().First()
					opts := gtfs.ParseRealtimeOptions{}
					rawExtension := ctx.String("extension")
					if err := readGtfsRealtimeExtension(rawExtension, &opts); err != nil {
						return err
					}

					source, err := journal.NewDirectoryGtfsrtSource(path)
					if err != nil {
						return fmt.Errorf("failed to open %s: %w", path, err)
					}
					fmt.Println("Building journal...")
					j := journal.BuildJournal(source, time.Unix(0, 0), time.Now())
					fmt.Println("Exporting journal to CSV format...")
					export, err := j.ExportToCsv()
					if err != nil {
						return fmt.Errorf("failed to export journal: %w", err)
					}

					outputDir := ctx.String("output")
					for _, f := range []struct {
						file string
						data []byte
					}{
						{
							file: "trips.csv",
							data: export.TripsCsv,
						},
						{
							file: "stop_times.csv",
							data: export.StopTimesCsv,
						},
					} {
						fullPath := filepath.Join(outputDir, f.file)
						fmt.Printf("Writing %s to %s\n", f.file, fullPath)
						if err := os.WriteFile(fullPath, f.data, 0666); err != nil {
							return fmt.Errorf("failed to write %s: %w", f.file, err)
						}
					}
					fmt.Println("Done")
					return nil
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func readGtfsRealtimeExtension(s string, opts *gtfs.ParseRealtimeOptions) error {
	switch s {
	case "":
	case "nycttrips":
		opts.Extension = nycttrips.Extension(nycttrips.ExtensionOpts{
			FilterStaleUnassignedTrips: true,
		})
		americaNewYorkTimezone, err := time.LoadLocation("America/New_York")
		if err == nil {
			opts.Timezone = americaNewYorkTimezone
		}
	case "nyctalerts":
		opts.Extension = nyctalerts.Extension(nyctalerts.ExtensionOpts{
			ElevatorAlertsDeduplicationPolicy:   nyctalerts.DeduplicateInComplex,
			ElevatorAlertsInformUsingStationIDs: true,
			SkipTimetabledNoServiceAlerts:       true,
		})
		americaNewYorkTimezone, err := time.LoadLocation("America/New_York")
		if err == nil {
			opts.Timezone = americaNewYorkTimezone
		}
	default:
		return fmt.Errorf("unknown extension %q; supported extensions are nycttrips and nyctalerts", s)
	}
	return nil
}

func formatTrip(trip gtfs.Trip, indent int, printStopTimes bool) string {
	var b strings.Builder
	tc := color.New(color.FgCyan)
	vc := color.New(color.FgMagenta)
	sc := color.New(color.FgGreen)
	newLine := fmt.Sprintf("\n%*s", indent, "")
	fmt.Fprintf(&b,
		"TripID %s  RouteID %s  DirectionID %s  StartDate %s  StartTime %s%s",
		tc.Sprint(trip.ID.ID),
		tc.Sprint(trip.ID.RouteID),
		tc.Sprint(trip.ID.DirectionID),
		tc.Sprint(trip.ID.StartDate.Format("2006-01-02")),
		tc.Sprint(trip.ID.StartTime),
		newLine,
	)
	if trip.Vehicle != nil {
		fmt.Fprintf(&b, "Vehicle: ID %s%s", vc.Sprint(trip.Vehicle.GetID().ID), newLine)
	} else {
		fmt.Fprintf(&b, "Vehicle: <none>%s", newLine)
	}

	if printStopTimes {
		fmt.Fprintf(&b, "Stop times (%d):%s", len(trip.StopTimeUpdates), newLine)
		for _, stopTime := range trip.StopTimeUpdates {
			fmt.Fprintf(&b,
				"  StopSeq %s  StopID %s  Arrival %s  Departure %s  NyctTrack %s%s",
				sc.Sprint(unPtrI(stopTime.StopSequence)),
				sc.Sprint(unPtr(stopTime.StopID)),
				unPtrT(stopTime.GetArrival().Time, sc),
				unPtrT(stopTime.GetDeparture().Time, sc),
				sc.Sprint(unPtr(stopTime.NyctTrack)),
				newLine,
			)
		}
	} else {
		fmt.Fprintf(&b, "Num stop times: %d (show with -v)%s", len(trip.StopTimeUpdates), newLine)
	}

	return b.String()
}

func formatAlert(alert gtfs.Alert, indent int) string {
	var header string
	for _, message := range alert.Header {
		if header == "" || len(message.Text) < len(header) {
			header = message.Text
		}
	}
	if len(header) > 100 {
		header = header[:100] + "..."
	}
	var b strings.Builder
	tc := color.New(color.FgCyan)
	vc := color.New(color.FgMagenta)
	// sc := color.New(color.FgGreen)
	newLine := fmt.Sprintf("\n%*s", indent, "")
	fmt.Fprintf(&b,
		"AlertID %s  Cause %s  Effect %s  Header %s%s",
		tc.Sprint(alert.ID),
		tc.Sprint(alert.Cause),
		tc.Sprint(alert.Effect),
		vc.Sprint(header),
		newLine,
	)
	return b.String()
}

func formatVehicle(vehicle gtfs.Vehicle, indent int) string {
	var b strings.Builder
	tc := color.New(color.FgCyan)
	vc := color.New(color.FgMagenta)
	sc := color.New(color.FgGreen)
	newLine := fmt.Sprintf("\n%*s", indent, "")
	fmt.Fprintf(&b,
		"VehicleID %s  Label %s  LicencePlate %s%s",
		tc.Sprint(vehicle.GetID().ID),
		tc.Sprint(vehicle.GetID().Label),
		tc.Sprint(vehicle.GetID().LicensePlate),
		newLine,
	)
	if vehicle.Trip != nil {
		fmt.Fprintf(&b, "Trip: ID %s%s", vc.Sprint(vehicle.Trip.ID), newLine)
	} else {
		fmt.Fprintf(&b, "Trip: <none>%s", newLine)
	}

	if vehicle.Position != nil {
		fmt.Fprintf(&b,
			"Position: Lat %s  Lon %s  Bearing %s  Odometer %s  Speed %s%s",
			sc.Sprint(unPtrF(vehicle.Position.Latitude)),
			sc.Sprint(unPtrF(vehicle.Position.Longitude)),
			sc.Sprint(unPtrF(vehicle.Position.Bearing)),
			sc.Sprint(unPtrF64(vehicle.Position.Odometer)),
			sc.Sprint(unPtrF(vehicle.Position.Speed)),
			newLine,
		)
	} else {
		fmt.Fprintf(&b, "Position: <none>%s", newLine)
	}

	fmt.Fprintf(&b, "CurrentStopSequence: %s%s", sc.Sprint(unPtrI(vehicle.CurrentStopSequence)), newLine)
	fmt.Fprintf(&b, "StopID: %s%s", sc.Sprint(unPtr(vehicle.StopID)), newLine)

	if vehicle.CurrentStatus != nil {
		fmt.Fprintf(&b, "CurrentStatus: %s%s", sc.Sprint(*vehicle.CurrentStatus), newLine)
	} else {
		fmt.Fprintf(&b, "CurrentStatus: <none>%s", newLine)
	}

	fmt.Fprintf(&b, "Timestamp: %s%s", sc.Sprint(unPtrT(vehicle.Timestamp, sc)), newLine)
	fmt.Fprintf(&b, "CongestionLevel: %s%s", sc.Sprint(vehicle.CongestionLevel), newLine)

	if vehicle.OccupancyStatus != nil {
		fmt.Fprintf(&b, "OccupancyStatus: %s%s", sc.Sprint(*vehicle.OccupancyStatus), newLine)
	} else {
		fmt.Fprintf(&b, "OccupancyStatus: <none>%s", newLine)
	}

	if vehicle.OccupancyPercentage != nil {
		fmt.Fprintf(&b, "OccupancyPercentage: %s%s", sc.Sprint(*vehicle.OccupancyPercentage), newLine)
	} else {
		fmt.Fprintf(&b, "OccupancyPercentage: <none>%s", newLine)
	}

	fmt.Fprintf(&b, "IsEntityInMessage: %t%s", vehicle.IsEntityInMessage, newLine)

	return b.String()
}

func unPtr(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}

func unPtrI(s *uint32) string {
	if s == nil {
		return "<none>"
	}
	return fmt.Sprintf("%d", s)
}

func unPtrF(s *float32) string {
	if s == nil {
		return "<none>"
	}
	return fmt.Sprintf("%f", *s)
}

func unPtrF64(s *float64) string {
	if s == nil {
		return "<none>"
	}
	return fmt.Sprintf("%f", *s)
}

func unPtrT(t *time.Time, c *color.Color) string {
	if t == nil {
		return "<none>"
	}
	return c.Sprint(t.String())
}
