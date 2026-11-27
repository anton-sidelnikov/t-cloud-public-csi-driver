package gpsfs

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/opentelekomcloud/gophertelekomcloud/openstack/gpfs"

	"t-cloud-public-csi-driver/internal/backend"
	backendgpsfs "t-cloud-public-csi-driver/internal/backend/gpsfs"
	"t-cloud-public-csi-driver/internal/config"
)

type fakeAPI struct {
	fileSystems []string
	rules       map[string][]gpfs.AccessRule
	createErr   error
	deleteErr   error
	getRulesErr error
	puts        int
	deleted     []string
}

func (f *fakeAPI) ListFS(context.Context) ([]gpfs.Bucket, error) {
	buckets := make([]gpfs.Bucket, 0, len(f.fileSystems))
	for _, name := range f.fileSystems {
		buckets = append(buckets, gpfs.Bucket{Name: name})
	}
	return buckets, nil
}

func (f *fakeAPI) CreateFS(_ context.Context, name, _ string) error {
	if f.createErr == nil {
		f.fileSystems = append(f.fileSystems, name)
	}
	return f.createErr
}

func (f *fakeAPI) DeleteFS(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	return f.deleteErr
}

func (f *fakeAPI) GetAccessRules(_ context.Context, name string) ([]gpfs.AccessRule, error) {
	if f.getRulesErr != nil {
		return nil, f.getRulesErr
	}
	rules, ok := f.rules[name]
	if !ok {
		return nil, apiError(404, codeNoSuchAccessRules)
	}
	return rules, nil
}

func (f *fakeAPI) PutAccessRules(_ context.Context, name string, rules []gpfs.AccessRule) error {
	f.puts++
	if f.rules == nil {
		f.rules = map[string][]gpfs.AccessRule{}
	}
	f.rules[name] = rules
	return nil
}

func apiError(statusCode int, code string) error {
	return gpfs.Error{BaseModel: gpfs.BaseModel{StatusCode: statusCode}, Code: code}
}

func vpcRule(vpcID string) gpfs.AccessRule {
	return gpfs.AccessRule{Action: gpfs.AccessRuleActionFullControl, Effect: gpfs.AccessRuleEffectAllow, Condition: gpfs.AccessRuleCondition{SourceVPC: vpcID}}
}

func testService(api fsAPI) *Service {
	return newService(config.Config{Region: "eu-de"}, api)
}

func TestServicePassesBackendValidation(t *testing.T) {
	if err := backend.Validate(backendgpsfs.New("eu-de"), testService(&fakeAPI{})); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestCreateVolumeAllowsVPC(t *testing.T) {
	api := &fakeAPI{}
	vol, err := testService(api).CreateVolume(context.Background(), backend.CreateVolumeRequest{Name: "pvc-1", SizeBytes: 10, VPCID: "vpc-1"})
	if err != nil {
		t.Fatalf("CreateVolume() error = %v", err)
	}
	if vol.ID != "pvc-1" || vol.SizeBytes != 10 || vol.ExportLocation != "pvc-1.sfs3.eu-de.otc.t-systems.com:/pvc-1" {
		t.Fatalf("CreateVolume() = %+v", vol)
	}
	if rules := api.rules["pvc-1"]; len(rules) != 1 || !reflect.DeepEqual(rules[0], vpcRule("vpc-1")) {
		t.Fatalf("access rules = %+v", rules)
	}
}

func TestCreateVolumeIsIdempotentAndKeepsRules(t *testing.T) {
	api := &fakeAPI{
		createErr: apiError(409, codeAlreadyOwnedByYou),
		rules:     map[string][]gpfs.AccessRule{"pvc-1": {vpcRule("vpc-other")}},
	}
	service := testService(api)
	for range 2 {
		if _, err := service.CreateVolume(context.Background(), backend.CreateVolumeRequest{Name: "pvc-1", VPCID: "vpc-1"}); err != nil {
			t.Fatalf("CreateVolume() error = %v", err)
		}
	}
	if rules := api.rules["pvc-1"]; len(rules) != 2 || !reflect.DeepEqual(rules[0], vpcRule("vpc-other")) || !reflect.DeepEqual(rules[1], vpcRule("vpc-1")) {
		t.Fatalf("access rules = %+v", rules)
	}
	if api.puts != 1 {
		t.Fatalf("access rules written %d times, want 1", api.puts)
	}
}

func TestCreateVolumeErrors(t *testing.T) {
	_, err := testService(&fakeAPI{createErr: apiError(409, codeNameTaken)}).CreateVolume(context.Background(), backend.CreateVolumeRequest{Name: "pvc-1", VPCID: "v"})
	if !errors.Is(err, backend.ErrVolumeAlreadyExists) {
		t.Fatalf("name of another account: error = %v, want ErrVolumeAlreadyExists", err)
	}
	_, err = testService(&fakeAPI{getRulesErr: apiError(403, "AccessDenied")}).CreateVolume(context.Background(), backend.CreateVolumeRequest{Name: "pvc-1", VPCID: "v"})
	if err == nil {
		t.Fatal("failed access rules lookup: CreateVolume() succeeded")
	}
}

func TestDeleteVolume(t *testing.T) {
	cases := map[string]struct {
		volumeID   string
		deleteErr  error
		wantErr    error
		wantDelete bool
	}{
		"deleted":        {volumeID: "pvc-1", wantDelete: true},
		"already gone":   {volumeID: "pvc-1", deleteErr: apiError(404, codeNoSuchFileSystem), wantDelete: true},
		"not empty":      {volumeID: "pvc-1", deleteErr: apiError(409, codeFileSystemNotEmpty), wantErr: backend.ErrVolumeOperationNotSupported, wantDelete: true},
		"subdirectory":   {volumeID: "pvc-1:data", wantErr: backend.ErrVolumeOperationNotSupported},
		"invalid ID":     {volumeID: "Invalid"},
		"service failed": {volumeID: "pvc-1", deleteErr: apiError(500, "InternalError"), wantErr: gpfs.Error{}, wantDelete: true},
	}
	for name, tc := range cases {
		api := &fakeAPI{deleteErr: tc.deleteErr}
		err := testService(api).DeleteVolume(context.Background(), tc.volumeID)
		switch {
		case tc.wantErr == nil && err != nil:
			t.Errorf("%s: DeleteVolume() error = %v", name, err)
		case tc.wantErr != nil && err == nil:
			t.Errorf("%s: DeleteVolume() succeeded, want error", name)
		case errors.Is(tc.wantErr, backend.ErrVolumeOperationNotSupported) && !errors.Is(err, tc.wantErr):
			t.Errorf("%s: DeleteVolume() error = %v, want %v", name, err, tc.wantErr)
		}
		if deleted := len(api.deleted) > 0; deleted != tc.wantDelete {
			t.Errorf("%s: file system deleted = %v, want %v", name, deleted, tc.wantDelete)
		}
	}
}

func TestGetVolume(t *testing.T) {
	service := testService(&fakeAPI{fileSystems: []string{"other", "pvc-1"}})

	vol, err := service.GetVolume(context.Background(), "pvc-1:data")
	if err != nil {
		t.Fatalf("GetVolume() error = %v", err)
	}
	if vol.ID != "pvc-1:data" || vol.Name != "pvc-1" || vol.ExportLocation != "pvc-1.sfs3.eu-de.otc.t-systems.com:/pvc-1" {
		t.Fatalf("GetVolume() = %+v", vol)
	}
	for _, id := range []string{"pvc-2", "Invalid"} {
		if _, err := service.GetVolume(context.Background(), id); !errors.Is(err, backend.ErrVolumeNotFound) {
			t.Errorf("GetVolume(%q) error = %v, want ErrVolumeNotFound", id, err)
		}
	}
}

func TestExpandVolumeReportsRequestedSize(t *testing.T) {
	size, err := testService(&fakeAPI{}).ExpandVolume(context.Background(), "pvc-1", 42)
	if err != nil || size != 42 {
		t.Fatalf("ExpandVolume() = %d, %v; want 42", size, err)
	}
}
