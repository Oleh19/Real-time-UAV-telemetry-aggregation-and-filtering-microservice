package partition_test

import (
	"testing"

	"uavmonitor/internal/partition"
)

func TestOfIsStableAndBounded(t *testing.T) {
	for _, count := range []int{1, 2, 4, 8} {
		p := partition.Of(50.45, 30.52, count)
		if p < 0 || p >= count {
			t.Fatalf("partition %d out of range for count %d", p, count)
		}
		if again := partition.Of(50.45, 30.52, count); again != p {
			t.Fatalf("partition not stable: %d vs %d", p, again)
		}
	}
}

func TestOfKeepsNearbyPointsTogether(t *testing.T) {
	base := partition.Of(50.400, 30.400, 4)
	near := partition.Of(50.405, 30.410, 4)
	if base != near {
		t.Fatalf("points ~1km apart landed in different partitions (%d vs %d)", base, near)
	}
}

func TestOfSpreadsDistantRegions(t *testing.T) {
	seen := map[int]bool{}
	points := [][2]float64{{50, 30}, {46, 34}, {49, 24}, {52, 38}, {44, 40}, {51, 22}}
	for _, pt := range points {
		seen[partition.Of(pt[0], pt[1], 4)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("all distant regions collapsed into %d partition(s)", len(seen))
	}
}

func TestSingleShardGetsWildcard(t *testing.T) {
	subjects := partition.AssignedSubjects("drone.telemetry", 4, 0, 1)
	if len(subjects) != 1 || subjects[0] != "drone.telemetry.*" {
		t.Fatalf("single shard subjects = %v, want the wildcard", subjects)
	}
}

func TestAssignedSubjectsPartitionEveryValueExactlyOnce(t *testing.T) {
	const partitions, shards = 8, 2
	covered := map[string]int{}
	for shard := range shards {
		for _, subject := range partition.AssignedSubjects("drone.telemetry", partitions, shard, shards) {
			covered[subject]++
		}
	}
	if len(covered) != partitions {
		t.Fatalf("covered %d partition subjects, want %d", len(covered), partitions)
	}
	for subject, times := range covered {
		if times != 1 {
			t.Fatalf("%s assigned to %d shards, want exactly 1", subject, times)
		}
	}
}
