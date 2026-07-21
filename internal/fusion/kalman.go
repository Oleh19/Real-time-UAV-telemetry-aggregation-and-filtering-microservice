package fusion

import (
	"math"
	"time"
)

const (
	initialVelocityStdDev = 60.0
	maxPredictStep        = 10.0
)

type kalmanFilter struct {
	originLatitude  float64
	originLongitude float64
	state           [4]float64
	covariance      [4][4]float64
	measurementVar  float64
	processAccel    float64
	lastTimestamp   time.Time
}

func newKalmanFilter(latitude, longitude float64, timestamp time.Time, measurementNoiseM, processAccel float64) *kalmanFilter {
	k := &kalmanFilter{
		originLatitude:  latitude,
		originLongitude: longitude,
		measurementVar:  measurementNoiseM * measurementNoiseM,
		processAccel:    processAccel,
		lastTimestamp:   timestamp,
	}
	k.covariance[0][0] = k.measurementVar
	k.covariance[1][1] = k.measurementVar
	k.covariance[2][2] = initialVelocityStdDev * initialVelocityStdDev
	k.covariance[3][3] = initialVelocityStdDev * initialVelocityStdDev
	return k
}

func (k *kalmanFilter) toLocal(latitude, longitude float64) (float64, float64) {
	x := (longitude - k.originLongitude) * metersPerDegreeEquator * math.Cos(k.originLatitude*math.Pi/180)
	y := (latitude - k.originLatitude) * metersPerDegreeEquator
	return x, y
}

func (k *kalmanFilter) toGeo(x, y float64) (float64, float64) {
	latitude := k.originLatitude + y/metersPerDegreeEquator
	longitude := k.originLongitude + x/(metersPerDegreeEquator*math.Cos(k.originLatitude*math.Pi/180))
	return latitude, longitude
}

func (k *kalmanFilter) predictedAt(timestamp time.Time) ([4]float64, [4][4]float64) {
	dt := timestamp.Sub(k.lastTimestamp).Seconds()
	if dt <= 0 {
		return k.state, k.covariance
	}
	if dt > maxPredictStep {
		dt = maxPredictStep
	}
	state := k.state
	state[0] += state[2] * dt
	state[1] += state[3] * dt

	covariance := k.covariance
	for row := range 2 {
		velocity := row + 2
		for col := range 4 {
			covariance[row][col] += dt * k.covariance[velocity][col]
		}
	}
	for col := range 2 {
		velocity := col + 2
		for row := range 4 {
			covariance[row][col] += dt * covariance[row][velocity]
		}
	}
	q := k.processAccel * k.processAccel
	dt2, dt3, dt4 := dt*dt, dt*dt*dt, dt*dt*dt*dt
	covariance[0][0] += q * dt4 / 4
	covariance[1][1] += q * dt4 / 4
	covariance[0][2] += q * dt3 / 2
	covariance[2][0] += q * dt3 / 2
	covariance[1][3] += q * dt3 / 2
	covariance[3][1] += q * dt3 / 2
	covariance[2][2] += q * dt2
	covariance[3][3] += q * dt2
	return state, covariance
}

func (k *kalmanFilter) mahalanobisSquared(latitude, longitude float64, timestamp time.Time) float64 {
	state, covariance := k.predictedAt(timestamp)
	zx, zy := k.toLocal(latitude, longitude)
	rx := zx - state[0]
	ry := zy - state[1]

	s00 := covariance[0][0] + k.measurementVar
	s01 := covariance[0][1]
	s10 := covariance[1][0]
	s11 := covariance[1][1] + k.measurementVar
	det := s00*s11 - s01*s10
	if det <= 0 {
		return math.Inf(1)
	}
	return (rx*(s11*rx-s01*ry) + ry*(s00*ry-s10*rx)) / det
}

func (k *kalmanFilter) update(latitude, longitude float64, timestamp time.Time) {
	state, covariance := k.predictedAt(timestamp)
	zx, zy := k.toLocal(latitude, longitude)
	rx := zx - state[0]
	ry := zy - state[1]

	s00 := covariance[0][0] + k.measurementVar
	s01 := covariance[0][1]
	s10 := covariance[1][0]
	s11 := covariance[1][1] + k.measurementVar
	det := s00*s11 - s01*s10
	if det <= 0 {
		return
	}
	inv00, inv01 := s11/det, -s01/det
	inv10, inv11 := -s10/det, s00/det

	var gain [4][2]float64
	for row := range 4 {
		gain[row][0] = covariance[row][0]*inv00 + covariance[row][1]*inv10
		gain[row][1] = covariance[row][0]*inv01 + covariance[row][1]*inv11
	}
	for row := range 4 {
		state[row] += gain[row][0]*rx + gain[row][1]*ry
	}
	var updated [4][4]float64
	for row := range 4 {
		for col := range 4 {
			updated[row][col] = covariance[row][col] -
				gain[row][0]*covariance[0][col] -
				gain[row][1]*covariance[1][col]
		}
	}
	k.state = state
	k.covariance = updated
	if timestamp.After(k.lastTimestamp) {
		k.lastTimestamp = timestamp
	}
}

func (k *kalmanFilter) position() (float64, float64) {
	return k.toGeo(k.state[0], k.state[1])
}

func (k *kalmanFilter) speed() float64 {
	return math.Hypot(k.state[2], k.state[3])
}

const metersPerDegreeEquator = 111320.0
