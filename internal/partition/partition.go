package partition

import (
	"fmt"
	"math"
)

const cellSizeDegrees = 1.0

func Of(latitude, longitude float64, count int) int {
	if count <= 1 {
		return 0
	}
	row := int(math.Floor((latitude + 90) / cellSizeDegrees))
	col := int(math.Floor((longitude + 180) / cellSizeDegrees))
	const cols = int(360/cellSizeDegrees) + 1
	cell := row*cols + col
	p := cell % count
	if p < 0 {
		p += count
	}
	return p
}

func Subject(base string, p int) string {
	return fmt.Sprintf("%s.%d", base, p)
}

func WildcardSubject(base string) string {
	return base + ".*"
}

func AssignedSubjects(base string, partitionCount, shardIndex, shardCount int) []string {
	if shardCount <= 1 {
		return []string{WildcardSubject(base)}
	}
	subjects := make([]string, 0, partitionCount/shardCount+1)
	for p := range partitionCount {
		if p%shardCount == shardIndex {
			subjects = append(subjects, Subject(base, p))
		}
	}
	return subjects
}

func AssignedPartitions(partitionCount, shardIndex, shardCount int) []int {
	if partitionCount < 1 {
		partitionCount = 1
	}
	if shardCount <= 1 {
		partitions := make([]int, partitionCount)
		for p := range partitionCount {
			partitions[p] = p
		}
		return partitions
	}
	partitions := make([]int, 0, partitionCount/shardCount+1)
	for p := range partitionCount {
		if p%shardCount == shardIndex {
			partitions = append(partitions, p)
		}
	}
	return partitions
}
