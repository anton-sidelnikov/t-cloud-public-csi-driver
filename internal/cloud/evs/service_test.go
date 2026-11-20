package evs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	blocksnapshots "github.com/opentelekomcloud/gophertelekomcloud/openstack/blockstorage/v3/snapshots"

	"t-cloud-public-csi-driver/internal/backend"
	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/config"
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

func TestBackendSnapshotMapping(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	snapshot := toBackendSnapshot(&blocksnapshots.Snapshot{
		ID:          "snap-1",
		Name:        "snap-name",
		Description: "desc",
		VolumeID:    "vol-1",
		Status:      "available",
		Size:        2,
		CreatedAt:   now,
	})

	if snapshot.ID != "snap-1" || snapshot.SourceVolumeID != "vol-1" {
		t.Fatalf("unexpected snapshot identity: %+v", snapshot)
	}
	if snapshot.SizeBytes != 2*gibibyte {
		t.Fatalf("unexpected snapshot size: %d", snapshot.SizeBytes)
	}
	if !snapshot.ReadyToUse {
		t.Fatal("expected snapshot to be ready to use")
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

// fakeBlockStorage serves volume create, get, and delete requests for CreateVolume tests.
type fakeBlockStorage struct {
	created  map[string]any
	metadata map[string]string
	deleted  int
}

func (f *fakeBlockStorage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/volumes"):
		var body struct {
			Volume map[string]any `json:"volume"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.created = body.Volume
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"volume":{"id":"vol-1","status":"creating"}}`))
	case r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"volume": map[string]any{
			"id": "vol-1", "status": "available", "size": 10, "metadata": f.metadata,
		}})
	case r.Method == http.MethodDelete:
		f.deleted++
		w.WriteHeader(http.StatusAccepted)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func testCreateService(t *testing.T, api *fakeBlockStorage) *Service {
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &golangsdk.ServiceClient{ProviderClient: &golangsdk.ProviderClient{}, Endpoint: server.URL + "/"}
	return &Service{cfg: config.Config{Timeout: time.Second}, blockStore: client}
}

const testKMSKeyID = "0d0466b0-e727-4d9c-b35d-f84bb474a37f"

func encryptedMetadata(keyID string) map[string]string {
	return map[string]string{backendevs.MetadataEncrypted: "1", backendevs.MetadataCMKID: keyID}
}

func TestCreateVolumeEncryptsWithKMSKey(t *testing.T) {
	api := &fakeBlockStorage{metadata: encryptedMetadata(testKMSKeyID)}
	req := backend.CreateVolumeRequest{Name: "pvc-1", SizeBytes: 10 * gibibyte, Metadata: map[string]string{"team": "a"}, KMSKeyID: testKMSKeyID}

	vol, err := testCreateService(t, api).CreateVolume(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	want := map[string]any{"team": "a", backendevs.MetadataEncrypted: "1", backendevs.MetadataCMKID: testKMSKeyID}
	if !reflect.DeepEqual(api.created["metadata"], want) {
		t.Fatalf("create metadata = %v, want %v", api.created["metadata"], want)
	}
	if len(req.Metadata) != 1 {
		t.Fatalf("request metadata modified: %v", req.Metadata)
	}
	if vol.KMSKeyID != testKMSKeyID || api.deleted != 0 {
		t.Fatalf("CreateVolume() = %+v, Delete calls = %d", vol, api.deleted)
	}
}

func TestCreateVolumeFromSnapshotOmitsEncryptionMetadata(t *testing.T) {
	api := &fakeBlockStorage{metadata: encryptedMetadata(testKMSKeyID)}
	req := backend.CreateVolumeRequest{Name: "pvc-1", SizeBytes: 10 * gibibyte, SnapshotID: "snap-1", KMSKeyID: testKMSKeyID}

	if _, err := testCreateService(t, api).CreateVolume(context.Background(), req); err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if md, _ := api.created["metadata"].(map[string]any); md[backendevs.MetadataEncrypted] != nil || md[backendevs.MetadataCMKID] != nil {
		t.Fatalf("create metadata = %v, want no encryption keys", md)
	}
}

func TestCreateVolumeDeletesVolumeWithoutRequestedEncryption(t *testing.T) {
	tests := map[string]struct {
		snapshotID string
		metadata   map[string]string
	}{
		"not encrypted":                  {metadata: nil},
		"encrypted flag off":             {metadata: map[string]string{backendevs.MetadataEncrypted: "0", backendevs.MetadataCMKID: testKMSKeyID}},
		"snapshot of another key":        {snapshotID: "snap-1", metadata: encryptedMetadata("1d0466b0-e727-4d9c-b35d-f84bb474a37f")},
		"snapshot of unencrypted volume": {snapshotID: "snap-1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			api := &fakeBlockStorage{metadata: tt.metadata}
			req := backend.CreateVolumeRequest{Name: "pvc-1", SizeBytes: 10 * gibibyte, SnapshotID: tt.snapshotID, KMSKeyID: testKMSKeyID}

			_, err := testCreateService(t, api).CreateVolume(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), "and was deleted") {
				t.Fatalf("CreateVolume() error = %v, want encryption mismatch", err)
			}
			if api.deleted != 1 {
				t.Fatalf("Delete calls = %d, want 1", api.deleted)
			}
		})
	}
}

func TestCreateVolumeWithoutKMSKeyAcceptsAnyEncryption(t *testing.T) {
	api := &fakeBlockStorage{metadata: encryptedMetadata(testKMSKeyID)}
	req := backend.CreateVolumeRequest{Name: "pvc-1", SizeBytes: 10 * gibibyte, SnapshotID: "snap-1"}

	vol, err := testCreateService(t, api).CreateVolume(context.Background(), req)
	if err != nil || api.deleted != 0 || vol.KMSKeyID != testKMSKeyID {
		t.Fatalf("CreateVolume() = %+v, %v; Delete calls = %d", vol, err, api.deleted)
	}
}
