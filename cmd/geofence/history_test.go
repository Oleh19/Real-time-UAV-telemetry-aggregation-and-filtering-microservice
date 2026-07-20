package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseHistoryQuery(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		droneID  string
		from     string
		to       string
		wantErr  string
		wantFrom time.Time
		wantTo   time.Time
	}{
		{
			name:    "missing drone id",
			wantErr: "drone_id",
		},
		{
			name:     "defaults to recent window",
			droneID:  "drone-001",
			wantFrom: now.Add(-defaultHistoryWindow),
			wantTo:   now,
		},
		{
			name:     "explicit range",
			droneID:  "drone-001",
			from:     "2026-07-21T10:00:00Z",
			to:       "2026-07-21T11:00:00Z",
			wantFrom: time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
			wantTo:   time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC),
		},
		{
			name:    "invalid from",
			droneID: "drone-001",
			from:    "yesterday",
			wantErr: "from",
		},
		{
			name:    "invalid to",
			droneID: "drone-001",
			to:      "later",
			wantErr: "to",
		},
		{
			name:    "inverted range",
			droneID: "drone-001",
			from:    "2026-07-21T11:00:00Z",
			to:      "2026-07-21T10:00:00Z",
			wantErr: "before",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			droneID, from, to, err := parseHistoryQuery(tc.droneID, tc.from, tc.to, now)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(droneID) != tc.droneID {
				t.Fatalf("droneID = %s, want %s", droneID, tc.droneID)
			}
			if !from.Equal(tc.wantFrom) || !to.Equal(tc.wantTo) {
				t.Fatalf("range = [%s, %s], want [%s, %s]", from, to, tc.wantFrom, tc.wantTo)
			}
		})
	}
}
