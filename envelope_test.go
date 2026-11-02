// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
	"github.com/eshu-hq/eshu-sdk/factschema"
)

func testZeroKey() marker.Key {
	return marker.Key{}
}

func testObservation() ResourceObservation {
	identity, err := ParseARMIdentity("/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01")
	if err != nil {
		panic(err)
	}
	return ResourceObservation{
		Boundary:      testBoundary(),
		ARMResourceID: "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01",
		Identity:      identity,
		Kind:          "",
		SKUClass:      "Standard",
		APIVersion:    "2023-09-01",
		Tags:          map[string]string{"env": "prod"},
		HasIdentity:   false,
		RawExtension:  map[string]any{"provisioningState": "Succeeded"},
	}
}

func TestNewResourceEnvelopeShape(t *testing.T) {
	t.Parallel()

	fact, err := NewResourceEnvelope(testObservation())
	if err != nil {
		t.Fatal(err)
	}
	if fact.Kind != factschema.FactKindAzureCloudResource {
		t.Errorf("Kind = %q", fact.Kind)
	}
	if fact.SchemaVersion != AzureCloudResourceSchemaVersion {
		t.Errorf("SchemaVersion = %q", fact.SchemaVersion)
	}
	if fact.SourceConfidence != sdk.SourceConfidenceReported {
		t.Errorf("SourceConfidence = %q", fact.SourceConfidence)
	}
	if strings.TrimSpace(fact.StableKey) == "" {
		t.Error("StableKey is empty")
	}
	if fact.SourceRef.FactKey != fact.StableKey {
		t.Error("SourceRef.FactKey must equal StableKey for the host contract")
	}
	if fact.SourceRef.SourceSystem != SourceSystem {
		t.Errorf("SourceRef.SourceSystem = %q", fact.SourceRef.SourceSystem)
	}
	payloadArm, _ := fact.Payload["arm_resource_id"].(string)
	if payloadArm == "" {
		t.Error("payload missing arm_resource_id")
	}
}

func TestEnvelopesAreIdempotent(t *testing.T) {
	t.Parallel()

	key := testRedactionKey(t)
	first, err := NewResourceEnvelope(testObservation())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewResourceEnvelope(testObservation())
	if err != nil {
		t.Fatal(err)
	}
	if first.StableKey != second.StableKey {
		t.Error("re-emission split the stable key")
	}
	firstTag, err := NewTagObservationEnvelope(testObservation(), key)
	if err != nil {
		t.Fatal(err)
	}
	secondTag, err := NewTagObservationEnvelope(testObservation(), key)
	if err != nil {
		t.Fatal(err)
	}
	if firstTag.StableKey != secondTag.StableKey {
		t.Error("tag re-emission split the stable key")
	}
}

func TestEnvelopeBuildersFailClosed(t *testing.T) {
	t.Parallel()

	key := testRedactionKey(t)
	observation := testObservation()
	if _, err := NewResourceEnvelope(ResourceObservation{}); err == nil {
		t.Error("empty observation built, want boundary error")
	}
	broken := observation
	broken.ARMResourceID = ""
	if _, err := NewResourceEnvelope(broken); err == nil {
		t.Error("missing arm id built, want error")
	}
	if _, err := NewTagObservationEnvelope(observation, testZeroKey()); err == nil {
		t.Error("keyless tag observation built, want error")
	}
	untagged := observation
	untagged.Tags = nil
	if _, err := NewTagObservationEnvelope(untagged, key); err == nil {
		t.Error("untagged observation built, want missing-evidence error")
	}
	if _, err := NewWarningEnvelope(WarningObservation{Boundary: testBoundary()}); err == nil {
		t.Error("warning without kind built, want error")
	}
	if _, err := NewRelationshipEnvelope(RelationshipObservation{Boundary: testBoundary()}); err == nil {
		t.Error("relationship without endpoints built, want error")
	}
	if _, err := NewRelationshipEnvelope(RelationshipObservation{
		Boundary: testBoundary(), SourceARMResourceID: observation.ARMResourceID,
		TargetARMResourceID: observation.ARMResourceID, RelationshipType: "managed_by",
		SupportState: "fabricated",
	}); err == nil {
		t.Error("fabricated support state built, want error")
	}
	if _, err := NewIdentityObservationEnvelope(IdentityObservation{
		Boundary: testBoundary(), ARMResourceID: observation.ARMResourceID,
		IdentityType: "fabricated",
		PrincipalID:  "11111111-1111-1111-1111-111111111111",
	}, key); err == nil {
		t.Error("fabricated identity type built, want error")
	}
	if _, err := NewDNSRecordEnvelope(DNSRecordObservation{Boundary: testBoundary()}, key); err == nil {
		t.Error("empty DNS observation built, want error")
	}
	if _, err := NewImageReferenceEnvelope(ImageReferenceObservation{
		Boundary: testBoundary(), OwningARMResourceID: observation.ARMResourceID,
	}, key); err == nil {
		t.Error("imageless reference built, want error")
	}
	if _, err := NewResourceChangeEnvelope(ResourceChangeObservation{
		Boundary: testBoundary(), TargetARMResourceID: observation.ARMResourceID,
		ChangeType: "fabricated", ChangeTime: time.Now(),
	}, key); err == nil {
		t.Error("fabricated change type built, want error")
	}
}

func TestWarningEnvelopeDefaultsOutcome(t *testing.T) {
	t.Parallel()

	fact, err := NewWarningEnvelope(WarningObservation{
		Boundary:    testBoundary(),
		WarningKind: WarningResultTruncated,
		Retryable:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fact.Payload["outcome"] != OutcomePartial {
		t.Errorf("outcome = %v, want default partial", fact.Payload["outcome"])
	}
	if fact.Kind != factschema.FactKindAzureCollectionWarning {
		t.Errorf("Kind = %q", fact.Kind)
	}
}

func TestBoundedChangedPropertyPathsTruncates(t *testing.T) {
	t.Parallel()

	var paths []string
	for i := 0; i < maxChangedPropertyPaths+10; i++ {
		paths = append(paths, "properties.field"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	bounded, truncated := boundedChangedPropertyPaths(paths)
	if !truncated {
		t.Error("truncated = false past the bound, want true")
	}
	if len(bounded) != maxChangedPropertyPaths {
		t.Errorf("len = %d, want %d", len(bounded), maxChangedPropertyPaths)
	}
}
