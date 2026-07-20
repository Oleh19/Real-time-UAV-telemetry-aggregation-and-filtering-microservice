package geofence

import "math"

type clusterPoint struct {
	latitude  float64
	longitude float64
}

type gridCell struct {
	row int
	col int
}

func clusterPoints(points []clusterPoint, radiusMeters float64, minSize int) [][]int {
	if len(points) == 0 || minSize < 1 {
		return nil
	}
	cellSizeDegrees := radiusMeters / metersPerDegreeLatitude
	grid := make(map[gridCell][]int, len(points))
	for n, point := range points {
		cell := cellOf(point, cellSizeDegrees)
		grid[cell] = append(grid[cell], n)
	}

	parent := make([]int, len(points))
	for n := range parent {
		parent[n] = n
	}
	var find func(int) int
	find = func(n int) int {
		if parent[n] != n {
			parent[n] = find(parent[n])
		}
		return parent[n]
	}
	union := func(a, b int) {
		rootA, rootB := find(a), find(b)
		if rootA != rootB {
			parent[rootB] = rootA
		}
	}

	for n, point := range points {
		cell := cellOf(point, cellSizeDegrees)
		for dRow := -1; dRow <= 1; dRow++ {
			for dCol := -1; dCol <= 1; dCol++ {
				neighbor := gridCell{row: cell.row + dRow, col: cell.col + dCol}
				for _, m := range grid[neighbor] {
					if m <= n {
						continue
					}
					if pointDistanceMeters(point, points[m]) <= radiusMeters {
						union(n, m)
					}
				}
			}
		}
	}

	groups := make(map[int][]int)
	for n := range points {
		root := find(n)
		groups[root] = append(groups[root], n)
	}
	var clusters [][]int
	for _, members := range groups {
		if len(members) >= minSize {
			clusters = append(clusters, members)
		}
	}
	return clusters
}

func cellOf(point clusterPoint, cellSizeDegrees float64) gridCell {
	return gridCell{
		row: int(math.Floor(point.latitude / cellSizeDegrees)),
		col: int(math.Floor(point.longitude / cellSizeDegrees)),
	}
}

const metersPerDegreeLatitude = 111320.0

func pointDistanceMeters(a, b clusterPoint) float64 {
	dLat := (b.latitude - a.latitude) * metersPerDegreeLatitude
	dLon := (b.longitude - a.longitude) * metersPerDegreeLatitude * math.Cos(a.latitude*math.Pi/180)
	return math.Hypot(dLat, dLon)
}
