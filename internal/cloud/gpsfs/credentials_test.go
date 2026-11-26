package gpsfs

import (
	"errors"
	"testing"
	"time"

	golangsdk "github.com/opentelekomcloud/gophertelekomcloud"
	"github.com/opentelekomcloud/gophertelekomcloud/openstack/identity/v3/credentials"

	"t-cloud-public-csi-driver/internal/config"
)

func TestTemporaryKeysAreRenewedBeforeExpiry(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	issued := 0
	keys := &temporaryKeys{
		now: func() time.Time { return now },
		issue: func() (accessKeys, error) {
			issued++
			return accessKeys{AccessKey: "ak", SecretKey: "sk", SecurityToken: "token", ExpiresAt: now.Add(time.Hour)}, nil
		},
	}

	for range 2 {
		if _, err := keys.Keys(); err != nil {
			t.Fatalf("Keys: %v", err)
		}
	}
	if issued != 1 {
		t.Fatalf("issued %d times, want 1", issued)
	}

	now = now.Add(time.Hour - temporaryCredentialsRefresh)
	if _, err := keys.Keys(); err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if issued != 2 {
		t.Fatalf("issued %d times after the refresh margin, want 2", issued)
	}
}

func TestTemporaryKeysErrorIsNotCached(t *testing.T) {
	fail := true
	keys := &temporaryKeys{
		now: time.Now,
		issue: func() (accessKeys, error) {
			if fail {
				return accessKeys{}, errors.New("forbidden")
			}
			return accessKeys{AccessKey: "ak", SecretKey: "sk", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Hour)}, nil
		},
	}
	if _, err := keys.Keys(); err == nil {
		t.Fatal("expected error")
	}
	fail = false
	if got, err := keys.Keys(); err != nil || got.AccessKey != "ak" {
		t.Fatalf("Keys = %+v, %v", got, err)
	}
}

func TestTemporaryCredential(t *testing.T) {
	got, err := temporaryCredential(&credentials.TemporaryCredential{
		AccessKey: "ak", SecretKey: "sk", SecurityToken: "token", ExpiresAt: "2026-11-26T13:00:00.123000Z",
	})
	if err != nil || got.ExpiresAt.Hour() != 13 || got.SecurityToken != "token" {
		t.Fatalf("temporaryCredential = %+v, %v", got, err)
	}
	for name, credential := range map[string]*credentials.TemporaryCredential{
		"nil":            nil,
		"no token":       {AccessKey: "ak", SecretKey: "sk", ExpiresAt: "2026-11-26T13:00:00Z"},
		"bad expires_at": {AccessKey: "ak", SecretKey: "sk", SecurityToken: "token", ExpiresAt: "soon"},
	} {
		if _, err := temporaryCredential(credential); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestNewKeySourceUsesConfiguredAccessKeys(t *testing.T) {
	source, err := newKeySource(config.Config{Backend: config.BackendGPSFS, AccessKey: "ak", SecretKey: "sk", SecurityToken: "token"}, golangsdk.AuthOptions{})
	if err != nil {
		t.Fatalf("newKeySource: %v", err)
	}
	keys, err := source.Keys()
	if err != nil || keys != (accessKeys{AccessKey: "ak", SecretKey: "sk", SecurityToken: "token"}) {
		t.Fatalf("Keys = %+v, %v", keys, err)
	}
}
