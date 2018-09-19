package main

import (
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/types"
)

// TestEstimatedHeightAt tests that the expectedHeightAt function correctly
// estimates the blockheight (and rounds to the nearest block).
func TestEstimatedHeightAt(t *testing.T) {
	tests := []struct {
		t              time.Time
		expectedHeight types.BlockHeight
	}{
		// Test on the same block that is used to estimate the height
		{
			time.Date(2018, time.August, 29, 4, 1, 50, 0, time.UTC),
			5000,
		},
		// 4 minutes later
		{
			time.Date(2018, time.August, 29, 4, 5, 50, 0, time.UTC),
			5000,
		},
		// 5 minutes later
		{
			time.Date(2018, time.August, 29, 4, 6, 50, 0, time.UTC),
			5000 + 1,
		},
		// 15 minutes later
		{
			time.Date(2018, time.August, 29, 4, 21, 50, 0, time.UTC),
			5000 + 2,
		},
		// 1 day later
		{
			time.Date(2018, time.August, 30, 4, 1, 50, 0, time.UTC),
			5000 + 160,
		},
	}
	for _, tt := range tests {
		h := estimatedHeightAt(tt.t)
		if h != tt.expectedHeight {
			t.Errorf("expected an estimated height of %v, but got %v", tt.expectedHeight, h)
		}
	}
}
