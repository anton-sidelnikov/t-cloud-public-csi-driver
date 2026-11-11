package driver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"

	backendevs "t-cloud-public-csi-driver/internal/backend/evs"
	"t-cloud-public-csi-driver/internal/config"
)

func newTestZoneResolver(url, fallback string) *metadataZoneResolver {
	return &metadataZoneResolver{url: url, client: http.DefaultClient, fallback: fallback, attempts: 2}
}

func TestMetadataZoneResolverPrefersInstanceZone(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"uuid":"x","availability_zone":"eu-de-02"}`))
	}))
	defer srv.Close()

	r := newTestZoneResolver(srv.URL, "eu-de-01")
	for i := 0; i < 2; i++ {
		zone, err := r.Resolve(context.Background())
		if err != nil || zone != "eu-de-02" {
			t.Fatalf("Resolve = %q, %v; want eu-de-02", zone, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cached result, metadata called %d times", calls.Load())
	}
}

func TestMetadataZoneResolverRetriesThenFallsBack(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	zone, err := newTestZoneResolver(srv.URL, "eu-de-01").Resolve(context.Background())
	if err != nil || zone != "eu-de-01" {
		t.Fatalf("Resolve = %q, %v; want eu-de-01", zone, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls.Load())
	}
}

func TestMetadataZoneResolverFailsWithoutAnyZone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"availability_zone":""}`))
	}))
	defer srv.Close()

	if zone, err := newTestZoneResolver(srv.URL, "").Resolve(context.Background()); err == nil {
		t.Fatalf("expected error, got zone %q", zone)
	}
}

type staticZoneResolver struct{ zone string }

func (r staticZoneResolver) Resolve(context.Context) (string, error) { return r.zone, nil }

func TestNodeGetInfoReportsResolvedZone(t *testing.T) {
	server := &nodeServer{
		cfg:            config.Config{AvailabilityZone: "eu-de-01"},
		driver:         backendevs.New(),
		nodeIDResolver: &staticNodeIDResolver{nodeID: "node-id"},
		zoneResolver:   staticZoneResolver{zone: "eu-de-02"},
	}

	resp, err := server.NodeGetInfo(context.Background(), &csi.NodeGetInfoRequest{})
	if err != nil {
		t.Fatalf("NodeGetInfo: %v", err)
	}
	if got := resp.GetAccessibleTopology().GetSegments()[server.driver.TopologyKey()]; got != "eu-de-02" {
		t.Fatalf("topology zone = %q, want eu-de-02", got)
	}
}
