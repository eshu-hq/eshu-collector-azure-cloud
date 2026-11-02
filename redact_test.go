// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu-sdk/collector/marker"
)

func testRedactionKey(t *testing.T) marker.Key {
	t.Helper()
	key, err := marker.NewKey([]byte("azure-satellite-test-redaction-key-material"))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestRedactExtensionDropsSecretsAndKeepsMetadata(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"provisioningState": "Succeeded",
		"hardwareProfile":   map[string]any{"vmSize": "Standard_D2s_v3"},
		"osProfile":         map[string]any{"adminPassword": "hunter2", "computerName": "vm-web-01"},
		"customData":        "base64-cloud-init-with-secrets",
		"connectionString":  "DefaultEndpointsProtocol=https;AccountKey=SECRET==",
		"defaultHostName":   "app-frontend.azurewebsites.net",
	}
	redacted := RedactExtension(raw)
	if !redacted.Redacted {
		t.Fatal("Redacted = false, want true for secret-bearing extension")
	}
	flattened := flattenKeys(t, redacted.Extension)
	for _, secret := range []string{"hunter2", "SECRET", "cloud-init", "azurewebsites.net"} {
		if strings.Contains(flattened, secret) {
			t.Errorf("redacted extension still carries %q", secret)
		}
	}
	if got := redacted.Extension["provisioningState"]; got != "Succeeded" {
		t.Errorf("safe metadata dropped: provisioningState = %v", got)
	}
	for _, want := range []string{"adminPassword", "customData", "connectionString", "defaultHostName"} {
		found := false
		for _, key := range redacted.RedactedKeys {
			if strings.EqualFold(key, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("RedactedKeys missing %q: %v", want, redacted.RedactedKeys)
		}
	}
}

func TestRedactExtensionNilStaysNil(t *testing.T) {
	t.Parallel()

	redacted := RedactExtension(nil)
	if redacted.Redacted || redacted.Extension != nil || len(redacted.RedactedKeys) != 0 {
		t.Errorf("nil input must yield a nil redaction: %+v", redacted)
	}
}

func TestFingerprintTagValuesMatchesSDKMarkerVector(t *testing.T) {
	t.Parallel()

	// The SDK pins this synthetic vector: the same key material, reason, and
	// source labels must produce the identical marker here, proving the
	// satellite fingerprints byte-identically to the in-tree collector.
	key, err := marker.NewKey([]byte("azure-tag-observation-test-key-material"))
	if err != nil {
		t.Fatal(err)
	}
	fingerprints, keys, truncated := FingerprintTagValues(map[string]string{"env": "prod"}, key)
	if truncated {
		t.Error("truncated = true for one tag, want false")
	}
	const want = "redacted:hmac-sha256:c78a3aed0e9e0e9662dc19a40eddcae8440730c99950d67b15453e8affde39a8"
	if fingerprints["env"] != want {
		t.Errorf("env fingerprint = %q, want SDK vector %q", fingerprints["env"], want)
	}
	if len(keys) != 1 || keys[0] != "env" {
		t.Errorf("keys = %v, want [env]", keys)
	}
}

func TestFingerprintTagValuesBoundsAndDropsBlankKeys(t *testing.T) {
	t.Parallel()

	key := testRedactionKey(t)
	tags := map[string]string{"  ": "blank", "env": "prod"}
	for i := 0; i < 60; i++ {
		tags["tag-"+string(rune('a'+i%26))+string(rune('0'+i/26))] = "value"
	}
	fingerprints, keys, truncated := FingerprintTagValues(tags, key)
	if !truncated {
		t.Error("truncated = false past the entry bound, want true")
	}
	if len(keys) != maxTagObservationEntries {
		t.Errorf("len(keys) = %d, want %d", len(keys), maxTagObservationEntries)
	}
	if _, ok := fingerprints["  "]; ok {
		t.Error("blank tag key fingerprinted, want dropped")
	}
	for k, v := range fingerprints {
		if !strings.HasPrefix(v, marker.Prefix) {
			t.Errorf("tag %q fingerprint %q lacks the marker prefix", k, v)
		}
	}
}

func TestFingerprintTagValuesEmptyIsMissingEvidence(t *testing.T) {
	t.Parallel()

	key := testRedactionKey(t)
	for _, tags := range []map[string]string{nil, {}, {"   ": "x"}} {
		if fingerprints, keys, truncated := FingerprintTagValues(tags, key); fingerprints != nil || keys != nil || truncated {
			t.Errorf("FingerprintTagValues(%v) = (%v, %v, %v), want all nil/false", tags, fingerprints, keys, truncated)
		}
	}
}

func flattenKeys(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ToLower(string(raw))
}
