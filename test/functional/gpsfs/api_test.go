//go:build functional

// Package gpsfsfunctional checks General Purpose SFS authentication and the file system
// API against the cloud. It does not need a cluster.
package gpsfsfunctional

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/gpfs"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/identity/v3/credentials"

	"t-cloud-public-csi-driver/internal/cloud/gpsfs"
	"t-cloud-public-csi-driver/internal/config"
	"t-cloud-public-csi-driver/test/functional/framework"
)

const fileSystemPrefix = "tcpcsi-func-"

// loadConfig reads the driver configuration of the gpsfs backend from the OS_* variables.
func loadConfig(t *testing.T) config.Config {
	t.Helper()

	for _, key := range []string{"OS_AUTH_URL", "OS_REGION", "OS_USERNAME", "OS_PASSWORD"} {
		framework.RequireEnv(t, key)
	}
	t.Setenv("CSI_BACKEND", config.BackendGPSFS)
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func authOptions(cfg config.Config) golangsdk.AuthOptions {
	return golangsdk.AuthOptions{
		IdentityEndpoint: cfg.AuthURL,
		Username:         cfg.UserName,
		Password:         cfg.Password,
		DomainName:       cfg.DomainName,
		AllowReauth:      true,
		TenantID:         cfg.ProjectID,
		TenantName:       cfg.ProjectName,
	}
}

func newClient(t *testing.T, cfg config.Config) *gpfs.Client {
	t.Helper()

	clients, err := gpsfs.NewClients(cfg, authOptions(cfg))
	if err != nil {
		t.Fatalf("NewClients: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	client, err := clients.Client(ctx)
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	return client
}

func listFileSystems(t *testing.T, client *gpfs.Client) []string {
	t.Helper()

	list, err := client.ListFS(&gpfs.ListFSInput{BucketType: "SFS"})
	if err != nil {
		t.Fatalf("list file systems: %v", err)
	}
	names := make([]string, 0, len(list.Buckets))
	for _, bucket := range list.Buckets {
		names = append(names, bucket.Name)
	}
	return names
}

func TestPasswordAuthenticationUsesTemporaryKeys(t *testing.T) {
	t.Setenv("OS_ACCESS_KEY", "")
	t.Setenv("OS_SECRET_KEY", "")
	t.Setenv("OS_SECURITY_TOKEN", "")
	cfg := loadConfig(t)

	t.Log("step: list file systems with temporary keys issued for the password credentials")
	listFileSystems(t, newClient(t, cfg))
}

// TestAccessKeyAuthenticationWithoutPassword uses OS_ACCESS_KEY and OS_SECRET_KEY when they
// are set, and temporary keys issued here otherwise. The driver gets no password.
func TestAccessKeyAuthenticationWithoutPassword(t *testing.T) {
	accessKey, secretKey, token := os.Getenv("OS_ACCESS_KEY"), os.Getenv("OS_SECRET_KEY"), os.Getenv("OS_SECURITY_TOKEN")
	if accessKey == "" || secretKey == "" {
		t.Setenv("OS_ACCESS_KEY", "")
		t.Setenv("OS_SECRET_KEY", "")
		t.Setenv("OS_SECURITY_TOKEN", "")
		t.Log("step: issue temporary access keys")
		credential := issueTemporaryKeys(t, loadConfig(t))
		accessKey, secretKey, token = credential.AccessKey, credential.SecretKey, credential.SecurityToken
	}
	framework.RequireEnv(t, "OS_REGION")
	for key, value := range map[string]string{
		"CSI_BACKEND": config.BackendGPSFS, "OS_ACCESS_KEY": accessKey, "OS_SECRET_KEY": secretKey, "OS_SECURITY_TOKEN": token,
		"OS_AUTH_URL": "", "OS_USERNAME": "", "OS_PASSWORD": "", "OS_PROJECT_ID": "", "OS_PROJECT_NAME": "",
	} {
		t.Setenv(key, value)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		t.Fatalf("load config without password: %v", err)
	}

	t.Log("step: list file systems with access keys only")
	listFileSystems(t, newClient(t, cfg))
}

func issueTemporaryKeys(t *testing.T, cfg config.Config) *credentials.TemporaryCredential {
	t.Helper()

	provider, err := openstack.AuthenticatedClient(authOptions(cfg))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	identity, err := openstack.NewIdentityV3(provider, golangsdk.EndpointOpts{})
	if err != nil {
		t.Fatalf("init IAM client: %v", err)
	}
	credential, err := credentials.CreateTemporary(identity, credentials.CreateTemporaryOpts{
		Methods: []string{"token"}, Token: provider.Token(), Duration: 900,
	}).Extract()
	if err != nil {
		t.Fatalf("create temporary access keys: %v", err)
	}
	return credential
}

func TestFileSystemLifecycle(t *testing.T) {
	cfg := loadConfig(t)
	vpcID := framework.RequireEnv(t, "CSI_TEST_VPC_ID")
	client := newClient(t, cfg)
	name := fmt.Sprintf("%s%d", fileSystemPrefix, time.Now().UnixNano())
	create := &gpfs.CreateFSInput{FSName: name, BucketType: "SFS", Redundancy: "3az", BucketLocation: gpfs.BucketLocation{Location: cfg.Region}}

	deleted := false
	t.Cleanup(func() {
		if deleted || os.Getenv("CSI_TEST_KEEP_RESOURCES") == "true" {
			return
		}
		if _, err := client.DeleteFS(name); err != nil && !hasCode(err, "NoSuchBucket") {
			t.Errorf("cleanup: delete file system %s: %v", name, err)
		}
	})

	t.Logf("step: create file system %s twice", name)
	for range 2 {
		if _, err := client.CreateFS(create); err != nil && !hasCode(err, "BucketAlreadyOwnedByYou") {
			t.Fatalf("create file system: %v", err)
		}
	}
	if names := listFileSystems(t, client); !slices.Contains(names, name) {
		t.Fatalf("file system %s is not listed: %v", name, names)
	}

	t.Log("step: allow access from the test VPC")
	_, err := client.CreateFSAccessRules(&gpfs.CreateFSAccessRulesInput{FSName: name, Statement: []gpfs.AccessRule{{
		Action: "FullControl", Effect: "Allow", Condition: gpfs.AccessRuleCondition{SourceVPC: vpcID},
	}}})
	if err != nil {
		t.Fatalf("configure access rules: %v", err)
	}
	rules, err := client.GetFSAccessRules(name)
	if err != nil {
		t.Fatalf("get access rules: %v", err)
	}
	if !slices.ContainsFunc(rules.Statement, func(rule gpfs.AccessRule) bool { return rule.Condition.SourceVPC == vpcID }) {
		t.Fatalf("access rules %+v do not allow VPC %s", rules.Statement, vpcID)
	}

	t.Log("step: delete file system twice")
	if _, err := client.DeleteFS(name); err != nil {
		t.Fatalf("delete file system: %v", err)
	}
	deleted = true
	if _, err := client.DeleteFS(name); !hasCode(err, "NoSuchBucket") {
		t.Fatalf("second delete error = %v, want NoSuchBucket", err)
	}
}

func hasCode(err error, code string) bool {
	var serviceErr gpfs.Error
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}
