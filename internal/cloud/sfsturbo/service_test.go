package sfsturbo

import (
	"context"
	"strings"
	"testing"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/sfs_turbo/v1/shares"

	"t-cloud-public-csi-driver/internal/backend"
	"t-cloud-public-csi-driver/internal/config"
)

// fakeAPI returns scripted Get results; the last result repeats once the script is exhausted.
type fakeAPI struct {
	listed    []shares.Turbo
	createErr error
	gets      []getResult
	created   []shares.CreateOpts
	deleted   []string
	expanded  []int
}

type getResult struct {
	share *shares.Turbo
	err   error
}

func (f *fakeAPI) Create(opts shares.CreateOpts) (*shares.TurboResponse, error) {
	f.created = append(f.created, opts)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &shares.TurboResponse{ID: "share-1", Name: opts.Name, Status: statusCreating}, nil
}

func (f *fakeAPI) Get(string) (*shares.Turbo, error) {
	next := f.gets[0]
	if len(f.gets) > 1 {
		f.gets = f.gets[1:]
	}
	return next.share, next.err
}

func (f *fakeAPI) List() ([]shares.Turbo, error) { return f.listed, nil }

func (f *fakeAPI) Delete(id string) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeAPI) Expand(_ string, newSizeGiB int) error {
	f.expanded = append(f.expanded, newSizeGiB)
	return nil
}

func share(status, subStatus, size, export string) getResult {
	return getResult{share: &shares.Turbo{
		ID: "share-1", Name: "pvc-1234", Status: status, SubStatus: subStatus, Size: size,
		ExportLocation: export, ShareType: "STANDARD", AvailabilityZone: "eu-de-01",
	}}
}

func notFound() getResult {
	return getResult{err: golangsdk.ErrDefault404{}}
}

func testService(api *fakeAPI) *Service {
	svc := newService(config.Config{Timeout: time.Second}, api)
	svc.pollInterval = time.Millisecond
	return svc
}

func createRequest() backend.CreateVolumeRequest {
	return backend.CreateVolumeRequest{
		Name: "pvc-1234", SizeBytes: 500 * gibibyte, AvailabilityZone: "eu-de-01", VolumeType: "STANDARD",
		VPCID: "vpc", SubnetID: "subnet", SecurityGroupID: "sg", KMSKeyID: "kms",
	}
}

func TestCreateVolumeWaitsForActiveShare(t *testing.T) {
	api := &fakeAPI{gets: []getResult{
		share(statusCreating, "", "500.00", ""),
		share(statusActive, "", "500.00", ""),
		share(statusActive, "", "500.00", "192.168.0.90:/"),
	}}

	vol, err := testService(api).CreateVolume(context.Background(), createRequest())
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if len(api.created) != 1 {
		t.Fatalf("Create calls = %d, want 1", len(api.created))
	}
	opts := api.created[0]
	if opts.Size != 500 || opts.ShareProto != "NFS" || opts.VpcID != "vpc" || opts.SubnetID != "subnet" ||
		opts.SecurityGroupID != "sg" || opts.Metadata.CryptKeyID != "kms" {
		t.Fatalf("CreateOpts = %+v", opts)
	}
	if vol.ID != "share-1" || vol.ExportLocation != "192.168.0.90:/" || vol.SizeBytes != 500*gibibyte || vol.VolumeType != "STANDARD" {
		t.Fatalf("CreateVolume() = %+v", vol)
	}
}

func TestCreateVolumeRetriesTransientGetErrors(t *testing.T) {
	api := &fakeAPI{gets: []getResult{
		{err: golangsdk.ErrDefault500{}},
		{err: golangsdk.ErrDefault404{}},
		share(statusActive, "", "500.00", "192.168.0.90:/"),
	}}

	vol, err := testService(api).CreateVolume(context.Background(), createRequest())
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if vol.ExportLocation != "192.168.0.90:/" {
		t.Fatalf("CreateVolume() = %+v", vol)
	}
}

func TestCreateVolumeHandlesNameConflict(t *testing.T) {
	listedLate := shares.Turbo{ID: "share-1", Name: "pvc-1234", Status: statusCreating, Size: "500.00"}
	tests := map[string]struct {
		lists   [][]shares.Turbo
		wantErr bool
	}{
		"share listed after conflict": {lists: [][]shares.Turbo{nil, {listedLate}}},
		"share still not listed":      {lists: [][]shares.Turbo{nil, nil}, wantErr: true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			api := &listSequenceAPI{
				fakeAPI: &fakeAPI{
					createErr: golangsdk.ErrDefault409{},
					gets:      []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")},
				},
				lists: tt.lists,
			}
			svc := newService(config.Config{Timeout: time.Second}, api)
			svc.pollInterval = time.Millisecond

			vol, err := svc.CreateVolume(context.Background(), createRequest())
			if tt.wantErr {
				if err == nil {
					t.Fatal("CreateVolume() error = nil, want conflict")
				}
				return
			}
			if err != nil || vol.ID != "share-1" {
				t.Fatalf("CreateVolume() = %+v, %v; want reuse of share-1", vol, err)
			}
		})
	}
}

// listSequenceAPI returns a different List result on each call.
type listSequenceAPI struct {
	*fakeAPI
	lists [][]shares.Turbo
	calls int
}

func (f *listSequenceAPI) List() ([]shares.Turbo, error) {
	i := min(f.calls, len(f.lists)-1)
	f.calls++
	return f.lists[i], nil
}

func TestCreateVolumeReusesShareWithSameName(t *testing.T) {
	api := &fakeAPI{
		listed: []shares.Turbo{{ID: "other", Name: "pvc-other"}, {ID: "share-1", Name: "pvc-1234", Status: statusCreating, Size: "500.00"}},
		gets:   []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")},
	}

	vol, err := testService(api).CreateVolume(context.Background(), createRequest())
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if len(api.created) != 0 || vol.ID != "share-1" {
		t.Fatalf("Create calls = %d, volume = %+v; want reuse of share-1", len(api.created), vol)
	}
}

func TestCreateVolumeRejectsSmallerExistingShare(t *testing.T) {
	api := &fakeAPI{listed: []shares.Turbo{{ID: "share-1", Name: "pvc-1234", Status: statusActive, Size: "500.00"}}}
	req := createRequest()
	req.SizeBytes = 600 * gibibyte

	if _, err := testService(api).CreateVolume(context.Background(), req); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("CreateVolume() error = %v, want already exists", err)
	}
}

func TestCreateVolumeDeletesFailedShare(t *testing.T) {
	api := &fakeAPI{gets: []getResult{share(statusCreateFailed, "", "", "")}}

	_, err := testService(api).CreateVolume(context.Background(), createRequest())
	if err == nil || !strings.Contains(err.Error(), "failed with status 303") {
		t.Fatalf("CreateVolume() error = %v, want creation failure", err)
	}
	if len(api.deleted) != 1 {
		t.Fatalf("Delete calls = %d, want 1", len(api.deleted))
	}
}

func TestDeleteVolume(t *testing.T) {
	tests := []struct {
		name        string
		gets        []getResult
		wantDeletes int
		wantErr     string
	}{
		{name: "already gone", gets: []getResult{notFound()}},
		{name: "waits until gone", gets: []getResult{share(statusActive, "", "500.00", ""), share(statusActive, "", "500.00", ""), notFound()}, wantDeletes: 1},
		{name: "still creating", gets: []getResult{share(statusCreating, "", "", "")}, wantErr: "still being created"},
		{name: "never disappears", gets: []getResult{share(statusActive, "", "500.00", "")}, wantDeletes: 1, wantErr: "timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{gets: tt.gets}
			svc := testService(api)
			svc.cfg.Timeout = 20 * time.Millisecond

			err := svc.DeleteVolume(context.Background(), "share-1")
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("DeleteVolume() error = %v, want %q", err, tt.wantErr)
			}
			if len(api.deleted) != tt.wantDeletes {
				t.Fatalf("Delete calls = %d, want %d", len(api.deleted), tt.wantDeletes)
			}
		})
	}
}

func TestExpandVolume(t *testing.T) {
	tests := []struct {
		name         string
		requestedGiB int64
		gets         []getResult
		wantExpand   []int
		wantGiB      int64
		wantErr      string
	}{
		{
			name:         "applies minimum increment",
			requestedGiB: 550,
			gets:         []getResult{share(statusActive, "", "500.00", ""), share(statusActive, subStatusExpanding, "500.00", ""), share(statusActive, "221", "600.00", "")},
			wantExpand:   []int{600},
			wantGiB:      600,
		},
		{
			name:         "already large enough",
			requestedGiB: 500,
			gets:         []getResult{share(statusActive, "221", "600.00", "")},
			wantGiB:      600,
		},
		{
			name:         "ignores stale failure substatus",
			requestedGiB: 700,
			gets:         []getResult{share(statusActive, subStatusExpandFailed, "500.00", ""), share(statusActive, subStatusExpandFailed, "500.00", ""), share(statusActive, "221", "700.00", "")},
			wantExpand:   []int{700},
			wantGiB:      700,
		},
		{
			name:         "reports failed expansion",
			requestedGiB: 700,
			gets:         []getResult{share(statusActive, "", "500.00", ""), share(statusActive, subStatusExpanding, "500.00", ""), share(statusActive, subStatusExpandFailed, "500.00", "")},
			wantExpand:   []int{700},
			wantErr:      "expansion failed",
		},
		{
			name:         "above maximum",
			requestedGiB: 40000,
			gets:         []getResult{share(statusActive, "", "500.00", "")},
			wantErr:      "maximum",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{gets: tt.gets}

			got, err := testService(api).ExpandVolume(context.Background(), "share-1", tt.requestedGiB*gibibyte)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ExpandVolume() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil || got != tt.wantGiB*gibibyte {
				t.Fatalf("ExpandVolume() = %d, %v; want %d GiB", got, err, tt.wantGiB)
			}
			if len(api.expanded) != len(tt.wantExpand) || len(tt.wantExpand) > 0 && api.expanded[0] != tt.wantExpand[0] {
				t.Fatalf("Expand calls = %v, want %v", api.expanded, tt.wantExpand)
			}
		})
	}
}
