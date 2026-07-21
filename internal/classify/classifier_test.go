package classify_test

import (
	"testing"
	"time"

	"uavmonitor/internal/classify"
	"uavmonitor/internal/telemetry"
)

func feed(c *classify.Classifier, id string, count int, speed float32, stepLat, stepLon func(n int) float64) telemetry.TargetClass {
	var class telemetry.TargetClass
	base := time.Now()
	for n := range count {
		class = c.Classify(telemetry.Sample{
			DroneID:   telemetry.DroneID(id),
			Timestamp: base.Add(time.Duration(n) * 500 * time.Millisecond),
			Latitude:  50.0 + stepLat(n),
			Longitude: 30.0 + stepLon(n),
			Altitude:  200,
			Speed:     speed,
		})
	}
	return class
}

func TestClassifierFastStraightTrackIsLoiteringMunition(t *testing.T) {
	c := classify.NewClassifier()
	class := feed(c, "target-001", 10, 380,
		func(n int) float64 { return float64(n) * 0.0018 },
		func(int) float64 { return 0 },
	)
	if class != telemetry.ClassLoiteringMunition {
		t.Fatalf("class = %s, want %s", class, telemetry.ClassLoiteringMunition)
	}
}

func TestClassifierSlowTrackIsMultirotor(t *testing.T) {
	c := classify.NewClassifier()
	class := feed(c, "target-002", 10, 70,
		func(n int) float64 {
			if n%2 == 0 {
				return float64(n) * 0.0003
			}
			return float64(n)*0.0003 + 0.0002
		},
		func(n int) float64 { return float64(n%3) * 0.0002 },
	)
	if class != telemetry.ClassMultirotor {
		t.Fatalf("class = %s, want %s", class, telemetry.ClassMultirotor)
	}
}

func TestClassifierMidSpeedTrackIsReconUAV(t *testing.T) {
	c := classify.NewClassifier()
	class := feed(c, "target-003", 10, 190,
		func(n int) float64 { return float64(n) * 0.0008 },
		func(n int) float64 { return float64(n) * 0.0002 },
	)
	if class != telemetry.ClassReconUAV {
		t.Fatalf("class = %s, want %s", class, telemetry.ClassReconUAV)
	}
}

func TestClassifierFastErraticTrackIsNotMunition(t *testing.T) {
	c := classify.NewClassifier()
	class := feed(c, "target-004", 12, 380,
		func(n int) float64 {
			if n%2 == 0 {
				return 0.002
			}
			return -0.002
		},
		func(n int) float64 {
			if n%3 == 0 {
				return -0.002
			}
			return 0.002
		},
	)
	if class == telemetry.ClassLoiteringMunition {
		t.Fatal("erratic fast track classified as loitering munition, heading stability was ignored")
	}
}

func TestClassifierNeedsEnoughSamples(t *testing.T) {
	c := classify.NewClassifier()
	class := feed(c, "target-005", 3, 380,
		func(n int) float64 { return float64(n) * 0.0018 },
		func(int) float64 { return 0 },
	)
	if class != telemetry.ClassUnknown {
		t.Fatalf("class after 3 samples = %s, want %s", class, telemetry.ClassUnknown)
	}
}

func TestClassifierCountsByClass(t *testing.T) {
	c := classify.NewClassifier()
	feed(c, "target-006", 10, 380,
		func(n int) float64 { return float64(n) * 0.0018 },
		func(int) float64 { return 0 },
	)
	feed(c, "target-007", 10, 70,
		func(n int) float64 { return float64(n%2) * 0.0002 },
		func(n int) float64 { return float64(n%3) * 0.0002 },
	)
	feed(c, "target-008", 2, 200,
		func(n int) float64 { return float64(n) * 0.0008 },
		func(int) float64 { return 0 },
	)

	counts := c.TrackedByClass()
	if counts[telemetry.ClassLoiteringMunition] != 1 {
		t.Errorf("munitions = %d, want 1", counts[telemetry.ClassLoiteringMunition])
	}
	if counts[telemetry.ClassMultirotor] != 1 {
		t.Errorf("multirotors = %d, want 1", counts[telemetry.ClassMultirotor])
	}
	if counts[telemetry.ClassUnknown] != 1 {
		t.Errorf("unknown = %d, want 1", counts[telemetry.ClassUnknown])
	}
}
