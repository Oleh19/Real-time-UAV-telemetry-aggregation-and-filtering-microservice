package broadcast_test

import (
	"sync"
	"testing"

	"uavmonitor/internal/broadcast"
	"uavmonitor/internal/telemetry"
)

func sampleFor() telemetry.Sample {
	return telemetry.Sample{DroneID: "target-001", Latitude: 50, Longitude: 30}
}

func TestHubDeliversToAllSubscribers(t *testing.T) {
	hub := broadcast.NewHub(4)
	_, first := hub.Subscribe()
	_, second := hub.Subscribe()

	hub.Broadcast(sampleFor())

	if got := (<-first).DroneID; got != "target-001" {
		t.Errorf("first subscriber got %s", got)
	}
	if got := (<-second).DroneID; got != "target-001" {
		t.Errorf("second subscriber got %s", got)
	}
	if hub.Delivered() != 2 {
		t.Errorf("Delivered = %d, want 2", hub.Delivered())
	}
}

func TestHubDropsForSlowSubscriberWithoutBlocking(t *testing.T) {
	hub := broadcast.NewHub(2)
	hub.Subscribe()

	for range 5 {
		hub.Broadcast(sampleFor())
	}

	if hub.Dropped() != 3 {
		t.Errorf("Dropped = %d, want 3 (buffer of 2)", hub.Dropped())
	}
	if hub.Delivered() != 2 {
		t.Errorf("Delivered = %d, want 2", hub.Delivered())
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	hub := broadcast.NewHub(4)
	id, ch := hub.Subscribe()

	hub.Unsubscribe(id)
	if _, open := <-ch; open {
		t.Fatal("channel still open after Unsubscribe")
	}
	if hub.Subscribers() != 0 {
		t.Fatalf("Subscribers = %d, want 0", hub.Subscribers())
	}
	hub.Broadcast(sampleFor())
}

func TestHubCloseTerminatesEverySubscriber(t *testing.T) {
	hub := broadcast.NewHub(4)
	_, first := hub.Subscribe()
	_, second := hub.Subscribe()

	hub.Close()

	if _, open := <-first; open {
		t.Error("first channel still open after Close")
	}
	if _, open := <-second; open {
		t.Error("second channel still open after Close")
	}
	if _, ch := hub.Subscribe(); func() bool { _, open := <-ch; return open }() {
		t.Error("Subscribe after Close returned an open channel")
	}
	hub.Broadcast(sampleFor())
}

func TestHubConcurrentBroadcastAndChurn(t *testing.T) {
	hub := broadcast.NewHub(8)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 200 {
				hub.Broadcast(sampleFor())
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 50 {
				id, ch := hub.Subscribe()
				for range 3 {
					select {
					case <-ch:
					default:
					}
				}
				hub.Unsubscribe(id)
			}
		})
	}
	wg.Wait()
}
