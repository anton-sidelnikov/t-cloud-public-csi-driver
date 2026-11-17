package sfsturbo

import (
	"strings"
	"testing"
)

func TestCapacityRulesCreateSizeGiB(t *testing.T) {
	tests := []struct {
		name         string
		expandType   string
		hpcBandwidth string
		requestedGiB int64
		want         int64
		wantErr      string
	}{
		{name: "standard minimum", requestedGiB: 1, want: 500},
		{name: "standard keeps size", requestedGiB: 777, want: 777},
		{name: "standard maximum", requestedGiB: 32769, wantErr: "maximum of 32768 GiB"},
		{name: "enhanced minimum", expandType: ExpandTypeBandwidth, requestedGiB: 500, want: 10240},
		{name: "enhanced maximum", expandType: ExpandTypeBandwidth, requestedGiB: 327681, wantErr: "maximum of 327680 GiB"},
		{name: "hpc minimum", expandType: ExpandTypeHPC, hpcBandwidth: "40M", requestedGiB: 1, want: 1228},
		{name: "hpc exact step", expandType: ExpandTypeHPC, hpcBandwidth: "125M", requestedGiB: 4915, want: 4915},
		{name: "hpc rounds up to 1.2 TiB steps", expandType: ExpandTypeHPC, hpcBandwidth: "1000M", requestedGiB: 1229, want: 2457},
		{name: "hpc 20M minimum", expandType: ExpandTypeHPC, hpcBandwidth: "20M", requestedGiB: 1228, want: 3686},
		{name: "hpc 8.4 TiB", expandType: ExpandTypeHPC, hpcBandwidth: "20M", requestedGiB: 8000, want: 8601},
		{name: "hpc maximum", expandType: ExpandTypeHPC, hpcBandwidth: "40M", requestedGiB: 1048500, wantErr: "maximum of 1048576 GiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CapacityRulesFor(tt.expandType, tt.hpcBandwidth).CreateSizeGiB(tt.requestedGiB)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreateSizeGiB() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("CreateSizeGiB() = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestCapacityRulesExpandSizeGiB(t *testing.T) {
	tests := []struct {
		name         string
		expandType   string
		currentGiB   int64
		requestedGiB int64
		want         int64
	}{
		{name: "standard minimum increment", currentGiB: 500, requestedGiB: 550, want: 600},
		{name: "standard larger request", currentGiB: 500, requestedGiB: 800, want: 800},
		{name: "enhanced minimum increment", expandType: ExpandTypeBandwidth, currentGiB: 10240, requestedGiB: 10241, want: 10340},
		{name: "hpc next step", expandType: ExpandTypeHPC, currentGiB: 1228, requestedGiB: 1300, want: 2457},
		{name: "hpc several steps", expandType: ExpandTypeHPC, currentGiB: 1228, requestedGiB: 4000, want: 4915},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CapacityRulesFor(tt.expandType, "40M").ExpandSizeGiB(tt.currentGiB, tt.requestedGiB)
			if err != nil || got != tt.want {
				t.Fatalf("ExpandSizeGiB() = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}
