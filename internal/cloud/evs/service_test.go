package evs

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"t-cloud-public-csi-driver/internal/backend"
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

func TestExpansionReadyStatus(t *testing.T) {
	if !expansionReadyStatus("available") {
		t.Fatal("expected available to be expansion-ready")
	}
	if !expansionReadyStatus("in-use") {
		t.Fatal("expected in-use to be expansion-ready")
	}
	if expansionReadyStatus("extending") {
		t.Fatal("did not expect extending to be expansion-ready")
	}
}

func TestHasAttachmentForServer(t *testing.T) {
	attachments := []backend.Attachment{
		{ID: "att-1", ServerID: "srv-1"},
		{ID: "att-2", ServerID: "srv-2"},
	}

	if !hasAttachmentForServer(attachments, "srv-2") {
		t.Fatal("expected attachment for srv-2")
	}
	if hasAttachmentForServer(attachments, "srv-3") {
		t.Fatal("did not expect attachment for srv-3")
	}
}

func TestWaitForAttachmentGoneReturnsWhenDetached(t *testing.T) {
	calls := 0
	err := waitForAttachmentGone(context.Background(), time.Second, time.Nanosecond, "vol-1", "srv-1", func(context.Context, string) (*backend.Volume, error) {
		calls++
		attachments := []backend.Attachment{{ID: "att-1", ServerID: "srv-1"}}
		if calls > 1 {
			attachments = nil
		}
		return &backend.Volume{ID: "vol-1", Attachments: attachments}, nil
	})
	if err != nil {
		t.Fatalf("waitForAttachmentGone returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("unexpected get volume calls: %d", calls)
	}
}

func TestWaitForAttachmentGoneTimesOut(t *testing.T) {
	err := waitForAttachmentGone(context.Background(), -time.Nanosecond, time.Nanosecond, "vol-1", "srv-1", func(context.Context, string) (*backend.Volume, error) {
		return &backend.Volume{ID: "vol-1", Attachments: []backend.Attachment{{ID: "att-1", ServerID: "srv-1"}}}, nil
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForAttachmentGonePropagatesGetVolumeError(t *testing.T) {
	err := waitForAttachmentGone(context.Background(), time.Second, time.Nanosecond, "vol-1", "srv-1", func(context.Context, string) (*backend.Volume, error) {
		return nil, fmt.Errorf("boom")
	})
	if err == nil {
		t.Fatal("expected get volume error")
	}
}

func TestWaitForAttachmentGoneHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForAttachmentGone(ctx, time.Second, time.Nanosecond, "vol-1", "srv-1", func(context.Context, string) (*backend.Volume, error) {
		return &backend.Volume{ID: "vol-1", Attachments: []backend.Attachment{{ID: "att-1", ServerID: "srv-1"}}}, nil
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
