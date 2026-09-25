package gtfs

import (
	"time"
)

// InterpolateStopTimes Helper: interpolate missing arrival or departure times evenly
func interpolateStopTimes(times []ScheduledStopTime) []ScheduledStopTime {
	// Work on a copy so the original slice is not modified outside
	result := make([]ScheduledStopTime, len(times))
	copy(result, times)
	n := len(result)
	if n == 0 {
		return nil
	}

	// Interpolate Arrival and Departure separately
	for tType := 0; tType < 2; tType++ {
		// tType: 0 = ArrivalTime, 1 = DepartureTime
		i := 0
		for i < n {
			// Find the next known time
			if getTime(&result[i], tType) == 0 {
				// Start of missing segment
				startIdx := i - 1
				startTime := time.Duration(0)
				if startIdx >= 0 {
					startTime = getTime(&result[startIdx], tType)
				}
				// Find end of missing segment
				endIdx := i
				for endIdx < n && getTime(&result[endIdx], tType) == 0 {
					endIdx++
				}
				endTime := time.Duration(0)
				if endIdx < n {
					endTime = getTime(&result[endIdx], tType)
				}
				intervals := endIdx - startIdx
				if startIdx >= 0 && endIdx < n && endTime > startTime && intervals > 0 {
					delta := (endTime - startTime) / time.Duration(intervals)
					for j := 1; j < intervals; j++ {
						setTime(&result[startIdx+j], tType, startTime+time.Duration(j)*delta)
					}
				}
				i = endIdx
			} else {
				i++
			}
		}
	}
	return result
}

// interpolateStopTimesByShapeDist fills missing arrival/departure times using
// shape_dist_traveled as the weight. A gap in which any record (including the
// two bounding timed records) has no distance falls back to even
// interpolation for that gap.
func interpolateStopTimesByShapeDist(times []ScheduledStopTime) []ScheduledStopTime {
	result := make([]ScheduledStopTime, len(times))
	copy(result, times)
	n := len(result)
	if n == 0 {
		return nil
	}

	for tType := 0; tType < 2; tType++ {
		i := 0
		for i < n {
			if getTime(&result[i], tType) != 0 {
				i++
				continue
			}
			startIdx := i - 1
			endIdx := i
			for endIdx < n && getTime(&result[endIdx], tType) == 0 {
				endIdx++
			}
			intervals := endIdx - startIdx
			if startIdx < 0 || endIdx >= n || intervals <= 0 {
				i = endIdx
				continue
			}
			startTime := getTime(&result[startIdx], tType)
			endTime := getTime(&result[endIdx], tType)
			if endTime <= startTime {
				i = endIdx
				continue
			}
			dists, ok := gapShapeDistances(result[startIdx : endIdx+1])
			if ok && dists[intervals] > dists[0] {
				span := dists[intervals] - dists[0]
				for j := 1; j < intervals; j++ {
					w := (dists[j] - dists[0]) / span
					setTime(&result[startIdx+j], tType, startTime+time.Duration(float64(endTime-startTime)*w))
				}
			} else {
				delta := (endTime - startTime) / time.Duration(intervals)
				for j := 1; j < intervals; j++ {
					setTime(&result[startIdx+j], tType, startTime+time.Duration(j)*delta)
				}
			}
			i = endIdx
		}
	}
	return result
}

// gapShapeDistances returns the shape distances of every record in gap, or
// false when any record has none.
func gapShapeDistances(gap []ScheduledStopTime) ([]float64, bool) {
	dists := make([]float64, len(gap))
	for i := range gap {
		if gap[i].ShapeDistanceTraveled == nil {
			return nil, false
		}
		dists[i] = *gap[i].ShapeDistanceTraveled
	}
	return dists, true
}

// Helpers to get/set arrival/departure by index
func getTime(stop *ScheduledStopTime, tType int) time.Duration {
	if tType == 0 {
		return stop.ArrivalTime
	}
	return stop.DepartureTime
}
func setTime(stop *ScheduledStopTime, tType int, t time.Duration) {
	if tType == 0 {
		stop.ArrivalTime = t
	} else {
		stop.DepartureTime = t
	}
}
