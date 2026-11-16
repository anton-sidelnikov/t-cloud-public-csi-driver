//go:build functional

// Command sfsturbo-cleanup deletes SFS Turbo shares that functional tests left in the
// test VPC. A leaked share blocks terraform destroy of the VPC and subnet.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/sfs_turbo/v1/shares"
)

const (
	// Shares provisioned by the CSI driver are named after the PV.
	sharePrefix   = "pvc-"
	statusDeleted = "400"
)

func main() {
	vpcID := flag.String("vpc-id", "", "delete driver-created shares in this VPC")
	timeout := flag.Duration("timeout", 20*time.Minute, "how long to wait for the shares to be deleted")
	flag.Parse()

	if err := run(*vpcID, *timeout); err != nil {
		log.Fatal(err)
	}
}

func run(vpcID string, timeout time.Duration) error {
	if vpcID == "" {
		return errors.New("-vpc-id is required")
	}

	provider, err := openstack.AuthenticatedClient(golangsdk.AuthOptions{
		IdentityEndpoint: os.Getenv("OS_AUTH_URL"),
		Username:         os.Getenv("OS_USERNAME"),
		Password:         os.Getenv("OS_PASSWORD"),
		DomainName:       os.Getenv("OS_DOMAIN_NAME"),
		TenantID:         os.Getenv("OS_PROJECT_ID"),
		TenantName:       os.Getenv("OS_PROJECT_NAME"),
	})
	if err != nil {
		return fmt.Errorf("authenticate: %w", err)
	}
	client, err := openstack.NewSharedFileSystemTurboV1(provider, golangsdk.EndpointOpts{Region: os.Getenv("OS_REGION")})
	if err != nil {
		return fmt.Errorf("init SFS Turbo client: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		list, err := shares.List(client, shares.ListOpts{})
		if err != nil {
			return fmt.Errorf("list SFS Turbo shares: %w", err)
		}

		var left []string
		for _, share := range list {
			if share.VpcID != vpcID || !strings.HasPrefix(share.Name, sharePrefix) || share.Status == statusDeleted {
				continue
			}
			left = append(left, share.ID)
			log.Printf("deleting leaked SFS Turbo share %s (%s), status %s", share.ID, share.Name, share.Status)
			if err := shares.Delete(client, share.ID); err != nil {
				// A share that is still being created or already being deleted rejects the request; retry on the next pass.
				log.Printf("delete SFS Turbo share %s: %v", share.ID, err)
			}
		}
		if len(left) == 0 {
			log.Printf("no SFS Turbo shares left in VPC %s", vpcID)
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("SFS Turbo shares still present in VPC %s after %s: %s", vpcID, timeout, strings.Join(left, ", "))
		}
		time.Sleep(30 * time.Second)
	}
}
