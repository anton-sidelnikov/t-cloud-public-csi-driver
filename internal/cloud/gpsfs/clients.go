package gpsfs

import (
	"context"
	"fmt"
	"net/http"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/gpfs"

	"t-cloud-public-csi-driver/internal/config"
)

// Endpoint returns the General Purpose SFS API endpoint of a region.
func Endpoint(region string) string {
	return "https://sfs3." + region + ".otc.t-systems.com"
}

// Clients creates General Purpose SFS API clients. The SDK binds the request context to
// the client, so every request gets its own client; they share one HTTP transport.
type Clients struct {
	keys      keySource
	endpoint  string
	transport *http.Transport
}

// NewClients authenticates with OS_ACCESS_KEY and OS_SECRET_KEY when they are set, and
// with temporary access keys issued for the OS_* password credentials otherwise.
func NewClients(cfg config.Config, authOpts golangsdk.AuthOptions) (*Clients, error) {
	keys, err := newKeySource(cfg, authOpts)
	if err != nil {
		return nil, err
	}
	return &Clients{
		keys:      keys,
		endpoint:  Endpoint(cfg.Region),
		transport: http.DefaultTransport.(*http.Transport).Clone(),
	}, nil
}

// Client returns a client for requests bound to ctx.
func (c *Clients) Client(ctx context.Context) (*gpfs.Client, error) {
	keys, err := c.keys.Keys()
	if err != nil {
		return nil, err
	}
	options := []gpfs.Configurer{gpfs.WithHttpTransport(c.transport), gpfs.WithRequestContext(ctx)}
	if keys.SecurityToken != "" {
		options = append(options, gpfs.WithSecurityToken(keys.SecurityToken))
	}
	client, err := gpfs.New(keys.AccessKey, keys.SecretKey, c.endpoint, options...)
	if err != nil {
		return nil, fmt.Errorf("init General Purpose SFS client: %w", err)
	}
	return client, nil
}
