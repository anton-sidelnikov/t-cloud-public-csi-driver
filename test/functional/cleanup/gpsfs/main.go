//go:build functional

// Command gpsfs-cleanup deletes SFS General Purpose file systems that functional tests left
// behind. Only file systems that allow access from the test VPC are considered, and only
// empty ones can be deleted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/gpfs"

	"t-cloud-public-csi-driver/internal/cloud/gpsfs"
	"t-cloud-public-csi-driver/internal/config"
)

// File systems provisioned by the CSI driver are named after the PV; the API tests use their own prefix.
var prefixes = []string{"pvc-", "tcpcsi-func-"}

func main() {
	vpcID := flag.String("vpc-id", "", "delete test file systems that allow access from this VPC")
	timeout := flag.Duration("timeout", 5*time.Minute, "how long the cleanup may take")
	flag.Parse()

	if err := run(*vpcID, *timeout); err != nil {
		log.Fatal(err)
	}
}

func run(vpcID string, timeout time.Duration) error {
	if vpcID == "" {
		return errors.New("-vpc-id is required")
	}
	if err := os.Setenv("CSI_BACKEND", config.BackendGPSFS); err != nil {
		return err
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	clients, err := gpsfs.NewClients(cfg, golangsdk.AuthOptions{
		IdentityEndpoint: cfg.AuthURL,
		Username:         cfg.UserName,
		Password:         cfg.Password,
		DomainName:       cfg.DomainName,
		AllowReauth:      true,
		TenantID:         cfg.ProjectID,
		TenantName:       cfg.ProjectName,
	})
	if err != nil {
		return fmt.Errorf("init SFS General Purpose clients: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client, err := clients.Client(ctx)
	if err != nil {
		return fmt.Errorf("init SFS General Purpose client: %w", err)
	}

	list, err := client.ListFS(&gpfs.ListFSInput{BucketType: "SFS"})
	if err != nil {
		return fmt.Errorf("list SFS General Purpose file systems: %w", err)
	}
	var left []string
	for _, bucket := range list.Buckets {
		name := bucket.Name
		if !slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(name, prefix) }) {
			continue
		}
		allowed, err := allowsVPC(client, name, vpcID)
		if err != nil {
			log.Printf("get access rules of SFS General Purpose file system %s: %v", name, err)
			left = append(left, name)
			continue
		}
		if !allowed {
			continue
		}
		log.Printf("deleting leaked SFS General Purpose file system %s", name)
		if _, err := client.DeleteFS(name); err != nil && !hasCode(err, "NoSuchBucket") {
			// A file system with data cannot be deleted through the API.
			log.Printf("delete SFS General Purpose file system %s: %v", name, err)
			left = append(left, name)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("SFS General Purpose file systems left for VPC %s: %s", vpcID, strings.Join(left, ", "))
	}
	log.Printf("no SFS General Purpose test file systems left for VPC %s", vpcID)
	return nil
}

func allowsVPC(client *gpfs.Client, name, vpcID string) (bool, error) {
	rules, err := client.GetFSAccessRules(name)
	if hasCode(err, "FsNoSuchSfsAclException") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(rules.Statement, func(rule gpfs.AccessRule) bool { return rule.Condition.SourceVPC == vpcID }), nil
}

func hasCode(err error, code string) bool {
	var serviceErr gpfs.Error
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}
