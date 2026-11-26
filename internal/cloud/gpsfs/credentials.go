package gpsfs

import (
	"fmt"
	"sync"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/identity/v3/credentials"

	"t-cloud-public-csi-driver/internal/config"
)

const (
	temporaryCredentialsDuration = time.Hour
	temporaryCredentialsRefresh  = 10 * time.Minute
)

// accessKeys are the AK/SK used to sign General Purpose SFS requests.
type accessKeys struct {
	AccessKey     string
	SecretKey     string
	SecurityToken string
	// ExpiresAt is zero for permanent keys.
	ExpiresAt time.Time
}

type keySource interface {
	Keys() (accessKeys, error)
}

// newKeySource uses OS_ACCESS_KEY and OS_SECRET_KEY when they are set. Otherwise it
// authenticates with the OS_* password credentials and issues temporary access keys.
func newKeySource(cfg config.Config, authOpts golangsdk.AuthOptions) (keySource, error) {
	if cfg.AccessKey != "" {
		return staticKeys{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, SecurityToken: cfg.SecurityToken}, nil
	}
	provider, err := openstack.AuthenticatedClient(authOpts)
	if err != nil {
		return nil, fmt.Errorf("authenticate against T Cloud Public: %w", err)
	}
	identity, err := openstack.NewIdentityV3(provider, golangsdk.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("init IAM client: %w", err)
	}
	return &temporaryKeys{issue: issueTemporaryKeys(provider, identity), now: time.Now}, nil
}

type staticKeys accessKeys

func (k staticKeys) Keys() (accessKeys, error) {
	return accessKeys(k), nil
}

// temporaryKeys issues temporary AK/SK with a security token and renews them shortly
// before they expire.
type temporaryKeys struct {
	issue func() (accessKeys, error)
	now   func() time.Time

	mu     sync.Mutex
	cached accessKeys
}

func (k *temporaryKeys) Keys() (accessKeys, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.cached.AccessKey != "" && k.now().Before(k.cached.ExpiresAt.Add(-temporaryCredentialsRefresh)) {
		return k.cached, nil
	}
	keys, err := k.issue()
	if err != nil {
		return accessKeys{}, fmt.Errorf("create temporary access keys: %w", err)
	}
	k.cached = keys
	return keys, nil
}

// issueTemporaryKeys creates temporary AK/SK from the token of the provider. The token in
// the request body is not renewed by the SDK, so an expired token is renewed and the call
// is repeated once.
func issueTemporaryKeys(provider *golangsdk.ProviderClient, identity *golangsdk.ServiceClient) func() (accessKeys, error) {
	create := func() (*credentials.TemporaryCredential, error) {
		return credentials.CreateTemporary(identity, credentials.CreateTemporaryOpts{
			Methods:  []string{"token"},
			Token:    provider.Token(),
			Duration: int(temporaryCredentialsDuration.Seconds()),
		}).Extract()
	}
	return func() (accessKeys, error) {
		credential, err := create()
		if err != nil && provider.ReauthFunc != nil {
			if reauthErr := provider.ReauthFunc(); reauthErr != nil {
				return accessKeys{}, fmt.Errorf("%w (re-authentication: %v)", err, reauthErr)
			}
			credential, err = create()
		}
		if err != nil {
			return accessKeys{}, err
		}
		return temporaryCredential(credential)
	}
}

func temporaryCredential(credential *credentials.TemporaryCredential) (accessKeys, error) {
	if credential == nil || credential.AccessKey == "" || credential.SecretKey == "" || credential.SecurityToken == "" {
		return accessKeys{}, fmt.Errorf("IAM returned incomplete temporary access keys")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, credential.ExpiresAt)
	if err != nil {
		return accessKeys{}, fmt.Errorf("parse expires_at %q: %w", credential.ExpiresAt, err)
	}
	return accessKeys{
		AccessKey:     credential.AccessKey,
		SecretKey:     credential.SecretKey,
		SecurityToken: credential.SecurityToken,
		ExpiresAt:     expiresAt,
	}, nil
}
