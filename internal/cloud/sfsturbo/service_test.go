package sfsturbo

import (
	"context"
	"errors"
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
	return &shares.TurboResponse{ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: opts.Name, Status: statusCreating}, nil
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
		ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: status, SubStatus: subStatus, Size: size,
		ExportLocation: export, ShareType: "STANDARD", AvailabilityZone: "eu-de-01", CryptKeyID: "kms",
	}}
}

func hpcShare(size, subStatus string) getResult {
	result := share(statusActive, subStatus, size, "")
	result.share.ExpandType, result.share.HpcBW = "hpc", "40M"
	return result
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
	if vol.ID != "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e" || vol.ExportLocation != "192.168.0.90:/" || vol.SizeBytes != 500*gibibyte || vol.VolumeType != "STANDARD" || vol.KMSKeyID != "kms" {
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
	listedLate := shares.Turbo{ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusCreating, Size: "500.00"}
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
			if err != nil || vol.ID != "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e" {
				t.Fatalf("CreateVolume() = %+v, %v; want reuse of 6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", vol, err)
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
		listed: []shares.Turbo{{ID: "other", Name: "pvc-other"}, {ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusCreating, Size: "500.00"}},
		gets:   []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")},
	}

	vol, err := testService(api).CreateVolume(context.Background(), createRequest())
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if len(api.created) != 0 || vol.ID != "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e" {
		t.Fatalf("Create calls = %d, volume = %+v; want reuse of 6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", len(api.created), vol)
	}
}

func TestCreateVolumePassesFileSystemTypeMetadata(t *testing.T) {
	api := &fakeAPI{gets: []getResult{share(statusActive, "", "1228.00", "192.168.0.90:/")}}
	req := createRequest()
	req.SizeBytes, req.ExpandType, req.HPCBandwidth = 1228*gibibyte, "hpc", "40M"

	if _, err := testService(api).CreateVolume(context.Background(), req); err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if md := api.created[0].Metadata; md.ExpandType != "hpc" || md.HpcBW != "40M" || md.CryptKeyID != "kms" || api.created[0].Size != 1228 {
		t.Fatalf("CreateOpts = %+v", api.created[0])
	}
}

func TestCreateVolumeChecksExistingShareAttributes(t *testing.T) {
	matching := func() shares.Turbo {
		return shares.Turbo{
			ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusActive, Size: "500.00", ShareType: "standard",
			AvailabilityZone: "eu-de-01", VpcID: "vpc", SubnetID: "subnet", SecurityGroupID: "sg", CryptKeyID: "kms",
		}
	}
	tests := map[string]struct {
		modify  func(*shares.Turbo)
		wantErr string
	}{
		"all attributes match":     {modify: func(*shares.Turbo) {}},
		"larger share is reused":   {modify: func(s *shares.Turbo) { s.Size = "600.00" }},
		"unreported attributes":    {modify: func(s *shares.Turbo) { s.SubnetID, s.CryptKeyID, s.AvailabilityZone = "", "", "" }},
		"smaller share":            {modify: func(s *shares.Turbo) { s.Size = "400.00" }, wantErr: "size 400 GiB"},
		"different share type":     {modify: func(s *shares.Turbo) { s.ShareType = "PERFORMANCE" }, wantErr: "share type"},
		"different zone":           {modify: func(s *shares.Turbo) { s.AvailabilityZone = "eu-de-02" }, wantErr: "availability zone"},
		"different VPC":            {modify: func(s *shares.Turbo) { s.VpcID = "other" }, wantErr: "VPC"},
		"different subnet":         {modify: func(s *shares.Turbo) { s.SubnetID = "other" }, wantErr: "subnet"},
		"different security group": {modify: func(s *shares.Turbo) { s.SecurityGroupID = "other" }, wantErr: "security group"},
		"different KMS key":        {modify: func(s *shares.Turbo) { s.CryptKeyID = "other" }, wantErr: "KMS key"},
		"different expand type":    {modify: func(s *shares.Turbo) { s.ExpandType = "bandwidth" }, wantErr: "expand type"},
		"different HPC bandwidth":  {modify: func(s *shares.Turbo) { s.HpcBW = "40M" }, wantErr: "HPC bandwidth"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			existing := matching()
			tt.modify(&existing)
			api := &fakeAPI{listed: []shares.Turbo{existing}, gets: []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")}}

			_, err := testService(api).CreateVolume(context.Background(), createRequest())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("CreateVolume() error = %v", err)
				}
				return
			}
			if !errors.Is(err, backend.ErrVolumeAlreadyExists) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CreateVolume() error = %v, want ErrVolumeAlreadyExists mentioning %q", err, tt.wantErr)
			}
			if len(api.created) != 0 {
				t.Fatalf("Create calls = %d, want 0", len(api.created))
			}
		})
	}
}

func TestCreateVolumeIgnoresShareTypeOfHPCShare(t *testing.T) {
	existing := shares.Turbo{
		ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusActive, Size: "1228.00", ShareType: "PERFORMANCE",
		ExpandType: "hpc", HpcBW: "40M",
	}
	api := &fakeAPI{listed: []shares.Turbo{existing}, gets: []getResult{share(statusActive, "", "1228.00", "192.168.0.90:/")}}
	req := createRequest()
	req.SizeBytes, req.ExpandType, req.HPCBandwidth = 1228*gibibyte, "hpc", "40M"

	if _, err := testService(api).CreateVolume(context.Background(), req); err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
}

func TestCreateVolumeChecksShareFoundAfterConflict(t *testing.T) {
	api := &listSequenceAPI{
		fakeAPI: &fakeAPI{createErr: golangsdk.ErrDefault409{}},
		lists:   [][]shares.Turbo{nil, {{ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusCreating, Size: "500.00", VpcID: "other"}}},
	}
	svc := newService(config.Config{Timeout: time.Second}, api)

	if _, err := svc.CreateVolume(context.Background(), createRequest()); !errors.Is(err, backend.ErrVolumeAlreadyExists) {
		t.Fatalf("CreateVolume() error = %v, want ErrVolumeAlreadyExists", err)
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

			err := svc.DeleteVolume(context.Background(), "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e")
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
		{
			name:         "hpc share grows in 1.2 TiB steps",
			requestedGiB: 1300,
			gets:         []getResult{hpcShare("1228.00", ""), hpcShare("1228.00", subStatusExpanding), hpcShare("2457.00", "221")},
			wantExpand:   []int{2457},
			wantGiB:      2457,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &fakeAPI{gets: tt.gets}

			got, err := testService(api).ExpandVolume(context.Background(), "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", tt.requestedGiB*gibibyte)
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

func TestCreateVolumeDeletesShareWithoutRequestedEncryption(t *testing.T) {
	unencrypted := share(statusActive, "", "500.00", "192.168.0.90:/")
	unencrypted.share.CryptKeyID = ""
	otherKey := share(statusActive, "", "500.00", "192.168.0.90:/")
	otherKey.share.CryptKeyID = "other"
	tests := map[string]struct {
		listed []shares.Turbo
		get    getResult
	}{
		"new share not encrypted":    {get: unencrypted},
		"new share with another key": {get: otherKey},
		"reused share not encrypted": {listed: []shares.Turbo{{ID: "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e", Name: "pvc-1234", Status: statusCreating}}, get: unencrypted},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			api := &fakeAPI{listed: tt.listed, gets: []getResult{tt.get}}

			_, err := testService(api).CreateVolume(context.Background(), createRequest())
			if err == nil || !strings.Contains(err.Error(), "instead of \"kms\" and was deleted") {
				t.Fatalf("CreateVolume() error = %v, want encryption mismatch", err)
			}
			if len(api.deleted) != 1 {
				t.Fatalf("Delete calls = %d, want 1", len(api.deleted))
			}
		})
	}
}

func TestCreateVolumeAcceptsEncryptedShareWithoutRequestedKey(t *testing.T) {
	api := &fakeAPI{gets: []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")}}
	req := createRequest()
	req.KMSKeyID = ""

	if _, err := testService(api).CreateVolume(context.Background(), req); err != nil || len(api.deleted) != 0 {
		t.Fatalf("CreateVolume() error = %v, Delete calls = %d", err, len(api.deleted))
	}
}

const testShareID = "6b9c1f2e-3d4a-4b5c-8d7e-9f0a1b2c3d4e"

func TestGetVolume(t *testing.T) {
	api := &fakeAPI{gets: []getResult{share(statusActive, "", "500.00", "192.168.0.90:/")}}
	vol, err := testService(api).GetVolume(context.Background(), testShareID+":data")
	if err != nil || vol.ID != testShareID+":data" || vol.ExportLocation != "192.168.0.90:/" || vol.Condition.Abnormal {
		t.Fatalf("GetVolume() = %+v, %v", vol, err)
	}

	for name, api := range map[string]*fakeAPI{
		"not found": {gets: []getResult{notFound()}},
		"deleted":   {gets: []getResult{share(statusDeleted, "", "500.00", "")}},
	} {
		if _, err := testService(api).GetVolume(context.Background(), testShareID); !errors.Is(err, backend.ErrVolumeNotFound) {
			t.Errorf("%s: GetVolume() error = %v, want ErrVolumeNotFound", name, err)
		}
	}
	if _, err := testService(&fakeAPI{}).GetVolume(context.Background(), "not-a-share"); !errors.Is(err, backend.ErrVolumeNotFound) {
		t.Errorf("GetVolume() of an invalid ID error = %v, want ErrVolumeNotFound", err)
	}
}

func TestSubdirectoryVolumesAreNotDeletedOrExpanded(t *testing.T) {
	api := &fakeAPI{gets: []getResult{share(statusActive, "", "500.00", "")}}
	svc := testService(api)
	if err := svc.DeleteVolume(context.Background(), testShareID+":data"); !errors.Is(err, backend.ErrVolumeOperationNotSupported) {
		t.Errorf("DeleteVolume() error = %v, want ErrVolumeOperationNotSupported", err)
	}
	if _, err := svc.ExpandVolume(context.Background(), testShareID+":data", 600*gibibyte); !errors.Is(err, backend.ErrVolumeOperationNotSupported) {
		t.Errorf("ExpandVolume() error = %v, want ErrVolumeOperationNotSupported", err)
	}
	if err := svc.DeleteVolume(context.Background(), "not-a-share"); err != nil {
		t.Errorf("DeleteVolume() of an invalid ID error = %v, want nil", err)
	}
	if len(api.deleted) != 0 || len(api.expanded) != 0 {
		t.Fatalf("Delete calls = %v, Expand calls = %v; want none", api.deleted, api.expanded)
	}
}
