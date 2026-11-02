// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/factschema"
)

// TestCollectSourceLanes proves the DNS and image lanes against the
// parity source-lanes fixture: supported DNS record sets and container-app
// images emit fingerprinted facts while SOA, empty, malformed, and AKS rows
// stay quiet without failing the scan.
func TestCollectSourceLanes(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_source_lanes.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceCount != 6 {
		t.Errorf("ResourceCount = %d, want 6", result.ResourceCount)
	}
	if result.DNSRecordCount != 1 {
		t.Errorf("DNSRecordCount = %d, want 1 (the valid CNAME only)", result.DNSRecordCount)
	}
	if result.ImageReferenceCount != 2 {
		t.Errorf("ImageReferenceCount = %d, want 2 (deduped digest + tag-only worker)", result.ImageReferenceCount)
	}
	if result.WarningCount != 0 {
		t.Errorf("WarningCount = %d, want 0 (quiet rows are not warnings)", result.WarningCount)
	}
	seenDigest, seenTag := false, false
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureImageReference {
			continue
		}
		switch fact.Payload["tag_digest_confidence"] {
		case ImageConfidenceDigest:
			seenDigest = true
		case ImageConfidenceTag:
			seenTag = true
		default:
			t.Errorf("image fact has confidence %v, want digest or tag", fact.Payload["tag_digest_confidence"])
		}
	}
	if !seenDigest || !seenTag {
		t.Error("want one digest-anchored and one tag-only image fact")
	}
	// DNS names never travel raw: the zone, record name, and every target are
	// fingerprinted while the bounded type and TTL stay as evidence.
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureDNSRecord {
			continue
		}
		if fact.Payload["record_type"] != "CNAME" {
			t.Errorf("record_type = %v, want CNAME", fact.Payload["record_type"])
		}
		name, _ := fact.Payload["record_name_fingerprint"].(string)
		if !strings.HasPrefix(name, "redacted:hmac-sha256:") {
			t.Errorf("record name fingerprint = %q, want a keyed marker", name)
		}
	}
}

// TestCollectRelationshipAndSystemIdentity proves the managed_by relationship
// and system-assigned identity emission on the parity
// relationship-identity fixture.
func TestCollectRelationshipAndSystemIdentity(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_relationship_identity.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceCount != 2 {
		t.Errorf("ResourceCount = %d, want 2", result.ResourceCount)
	}
	if result.RelationshipCount != 1 {
		t.Errorf("RelationshipCount = %d, want 1 (vmss managed_by)", result.RelationshipCount)
	}
	if result.IdentityObservationCount != 1 {
		t.Errorf("IdentityObservationCount = %d, want 1 (system-assigned)", result.IdentityObservationCount)
	}
}

// TestCollectUserAssignedIdentities proves one observation per user-assigned
// identity plus the system-assigned one, with every principal GUID
// fingerprinted.
func TestCollectUserAssignedIdentities(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_user_assigned_identity.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if result.IdentityObservationCount != 3 {
		t.Errorf("IdentityObservationCount = %d, want 3 (system + 2 user-assigned)", result.IdentityObservationCount)
	}
	seenUser := 0
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureIdentityObservation {
			continue
		}
		if fact.Payload["identity_type"] == IdentityTypeUserAssigned {
			seenUser++
		}
		for _, field := range []string{"principal_fingerprint", "client_fingerprint", "tenant_fingerprint"} {
			marker, _ := fact.Payload[field].(string)
			if marker != "" && !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
				t.Errorf("identity field %s = %q, want a keyed marker or absence", field, marker)
			}
		}
	}
	if seenUser != 2 {
		t.Errorf("user-assigned observations = %d, want 2", seenUser)
	}
}

// TestCollectResourceChangesLane proves the fixture-only resourcechanges lane:
// create/update/delete freshness evidence emits one fact per change, a tombstone
// candidate for the delete, and the actor fingerprint instead of the raw actor.
func TestCollectResourceChangesLane(t *testing.T) {
	t.Parallel()

	target := testTarget()
	target.SourceLane = SourceLaneResourceChanges
	config := Config{CollectorInstanceID: "azure-instance-1", Targets: []TargetConfig{target}}
	provider, err := NewFixtureResourceChangesPageProviderFromFiles(ScopeAccess{}, "testdata/resourcechanges_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	source := &Source{
		Config:          config,
		ProviderFactory: StaticFixtureFactory(provider),
		Metrics:         NewCountingMetrics(),
		RedactionKey:    testRedactionKey(t),
	}
	claim := testClaim()
	scopeID, err := ScopeIDForTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	claim.Scope.ID = scopeID
	result, err := source.Collect(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete for the two-row changes page", result.State)
	}
	if len(result.Facts) != 2 {
		t.Fatalf("facts = %d, want 2 (one per change)", len(result.Facts))
	}
	changes := 0
	seenTombstone := false
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureResourceChange {
			continue
		}
		changes++
		if tombstone, _ := fact.Payload["is_tombstone_candidate"].(bool); tombstone {
			seenTombstone = true
		}
	}
	if changes != 2 {
		t.Errorf("resource_change facts = %d, want 2", changes)
	}
	if !seenTombstone {
		t.Error("delete change lacks the tombstone-candidate flag")
	}
	// The changes lane requires a redaction key: keyless collection fails
	// closed instead of emitting unfingerprinted actor evidence.
	keyless := &Source{Config: config, ProviderFactory: StaticFixtureFactory(provider)}
	if _, err := keyless.Collect(context.Background(), claim); err == nil {
		t.Error("keyless resourcechanges collection succeeded, want the redaction-key gate")
	}
}
