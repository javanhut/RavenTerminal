package main

import "testing"

func TestDragThresholdPassedInAnyDirection(t *testing.T) {
	tests := []struct {
		name       string
		x, y       float64
		wantPassed bool
	}{
		{name: "still a click", x: 3, y: 4, wantPassed: false},
		{name: "horizontal drag", x: 6, y: 0, wantPassed: true},
		{name: "vertical drag", x: 0, y: -6, wantPassed: true},
		{name: "diagonal drag", x: 4, y: 4, wantPassed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dragThresholdPassed(0, 0, tt.x, tt.y, 5); got != tt.wantPassed {
				t.Fatalf("dragThresholdPassed() = %v, want %v", got, tt.wantPassed)
			}
		})
	}
}
