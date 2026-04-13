package evs

import (
	"net/http"
	"reflect"
	"testing"
)

func TestSizeBytesToGiB(t *testing.T) {
	testCases := []struct {
		name  string
		bytes int64
		want  int64
	}{
		{name: "zero rounds to minimum one", bytes: 0, want: 1},
		{name: "one byte rounds up", bytes: 1, want: 1},
		{name: "exact gibibyte stays one", bytes: gibibyte, want: 1},
		{name: "fractional gibibyte rounds up", bytes: gibibyte + 1, want: 2},
		{name: "multiple gibibytes preserved", bytes: 5 * gibibyte, want: 5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sizeBytesToGiB(tc.bytes); got != tc.want {
				t.Fatalf("sizeBytesToGiB(%d) = %d, want %d", tc.bytes, got, tc.want)
			}
		})
	}
}

func TestOKCodes(t *testing.T) {
	testCases := []struct {
		method string
		want   []int
	}{
		{method: http.MethodPost, want: []int{http.StatusAccepted, http.StatusCreated, http.StatusOK}},
		{method: http.MethodDelete, want: []int{http.StatusAccepted, http.StatusNoContent, http.StatusNotFound}},
		{method: http.MethodGet, want: []int{http.StatusOK}},
	}

	for _, tc := range testCases {
		t.Run(tc.method, func(t *testing.T) {
			if got := okCodes(tc.method); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("okCodes(%q) = %v, want %v", tc.method, got, tc.want)
			}
		})
	}
}

func TestVolumePayloadToDomain(t *testing.T) {
	payload := volumePayload{
		ID:               "vol-1",
		Name:             "data",
		Status:           "available",
		AvailabilityZone: "eu-de-01",
		VolumeType:       "SSD",
		SizeGiB:          3,
		Attachments: []attachmentPayload{
			{ID: "att-1", ServerID: "srv-1", VolumeID: "vol-1", Device: "/dev/vdb"},
		},
	}

	volume := payload.toDomain()
	if volume.ID != "vol-1" || volume.Name != "data" {
		t.Fatalf("unexpected volume identity: %+v", volume)
	}
	if volume.SizeBytes != 3*gibibyte {
		t.Fatalf("unexpected volume size: %d", volume.SizeBytes)
	}
	if len(volume.Attachments) != 1 || volume.Attachments[0].Device != "/dev/vdb" {
		t.Fatalf("unexpected attachments: %+v", volume.Attachments)
	}
}
