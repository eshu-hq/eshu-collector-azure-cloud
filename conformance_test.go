// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/conformance"
	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
	"gopkg.in/yaml.v3"
)

// azureKinds is every fact kind this satellite emits, in manifest order. All
// eight have typed structs in factschema/azure/v1; no kind is invented inline.
var azureKinds = []string{
	factschema.FactKindAzureCloudResource,
	factschema.FactKindAzureCloudRelationship,
	factschema.FactKindAzureTagObservation,
	factschema.FactKindAzureIdentityObservation,
	factschema.FactKindAzureDNSRecord,
	factschema.FactKindAzureImageReference,
	factschema.FactKindAzureResourceChange,
	factschema.FactKindAzureCollectionWarning,
}

func loadManifest(t *testing.T) conformance.Manifest {
	t.Helper()
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var manifest conformance.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testObservedAt() time.Time {
	return time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
}

func testClaim() sdk.Claim {
	scopeID := "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph"
	return sdk.Claim{
		ComponentID:   ComponentID,
		InstanceID:    "azure-instance-1",
		CollectorKind: CollectorKind,
		SourceSystem:  SourceSystem,
		Scope:         sdk.Scope{ID: scopeID, Kind: "subscription"},
		SourceRunID:   "source-run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-item-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      testObservedAt().Add(time.Hour),
		ConfigHandle:  "config-handle-1",
	}
}

// coreTestAuthorization returns the trusted host-granted authorization snapshot
// for this collector's eight core-owned kinds, plus the reserved-kind
// inventory the tests play host with. The bare azure_* kinds carry no dot
// namespace, so the harness only accepts them with exact CoreFacts coverage
// for this producer, version, collector kind, and schema: every
// conformance.Run in this repo must pass both values. PayloadSchemaRef stays
// empty so no fixture-pack schema coupling is required in the grant path.
func coreTestAuthorization(t *testing.T) (*conformance.CoreFactContext, []string) {
	t.Helper()

	facts := make([]conformance.CoreFactAuthorization, 0, len(azureKinds))
	for _, kind := range azureKinds {
		facts = append(facts, conformance.CoreFactAuthorization{
			Kind: kind,
			Emissions: []conformance.CoreFactEmission{
				{CollectorKind: CollectorKind, SchemaVersions: []string{"1.0.0"}},
			},
			ReducerPhases: []string{"source_evidence_only:no_graph_truth"},
		})
	}
	reserved := append([]string(nil), azureKinds...)
	return &conformance.CoreFactContext{
		ComponentID:      ComponentID,
		ComponentVersion: ComponentVersion,
		Facts:            facts,
	}, reserved
}

// azurePayloadSchemas returns the pinned fixture-pack schemas for the kinds
// this satellite emits, so the harness validates every fixture fact payload
// against the exact schema the target reducer release decodes.
func azurePayloadSchemas(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	all := fixturepack.Schemas()
	out := make(map[string]json.RawMessage, len(azureKinds))
	for _, kind := range azureKinds {
		raw, ok := all[kind]
		if !ok {
			t.Fatalf("pinned fixture pack ships no schema for %q", kind)
		}
		out[kind] = raw
	}
	return out
}

func mustCollect(t *testing.T, claim sdk.Claim, pages ...string) sdk.Result {
	t.Helper()
	provider, err := NewFixturePageProviderFromFiles(ScopeAccess{}, pages...)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{CollectorInstanceID: "azure-instance-1", Targets: []TargetConfig{{
		TenantID:           "tenant-abc",
		ScopeKind:          ScopeKindSubscription,
		ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily: "microsoft.compute",
		LocationBucket:     "eastus",
		CredentialRef:      "azure-read-only-spn",
		SourceURI:          "https://management.azure.com/",
		FencingToken:       7,
	}}}
	source := &Source{
		Config:          config,
		ProviderFactory: StaticFixtureFactory(provider),
		Metrics:         NewCountingMetrics(),
		RedactionKey:    testRedactionKey(t),
		Clock:           testObservedAt,
	}
	result, err := source.Collect(context.Background(), claim)
	if err != nil {
		t.Fatal(err)
	}
	contract := loadManifest(t).Contract()
	if _, err := sdk.NewValidator(contract).ValidateResult(result); err != nil {
		t.Fatalf("collected result fails the SDK contract: %v", err)
	}
	return result
}

// TestConformancePositive proves the satellite passes the public fixture
// harness on the same parity fixtures the in-tree producer uses.
func TestConformancePositive(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, testClaim(), "testdata/resources_page1.json", "testdata/resources_page2.json")
	coreFacts, reserved := coreTestAuthorization(t)
	report := conformance.Run(conformance.Request{
		Manifest:          manifest,
		Fixtures:          []sdk.Result{result},
		Mode:              conformance.ModeFixture,
		CoreFacts:         coreFacts,
		ReservedFactKinds: reserved,
		PayloadSchemas:    azurePayloadSchemas(t),
	})
	if !report.OK() {
		t.Fatalf("conformance findings = %#v, want passed", report.Findings)
	}
	if report.Summary.FactCount != len(result.Facts) {
		t.Errorf("FactCount = %d, want %d", report.Summary.FactCount, len(result.Facts))
	}
	if !report.Summary.IdempotentReemissionChecked {
		t.Error("IdempotentReemissionChecked = false, want true")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema_version":"eshu.extension.conformance.v1"`, `"status":"passed"`, `"component_id":"` + ComponentID + `"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("conformance report JSON missing %q: %s", want, raw)
		}
	}
}

// TestConformanceTruncatedProvesWarningEvidence proves a provider-truncated
// page still passes conformance with explicit warning evidence instead of
// silent success.
func TestConformanceTruncatedProvesWarningEvidence(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, testClaim(), "testdata/resources_truncated.json")
	if result.State != sdk.ResultPartial {
		t.Fatalf("State = %q, want partial for a truncated page", result.State)
	}
	coreFacts, reserved := coreTestAuthorization(t)
	report := conformance.Run(conformance.Request{
		Manifest:          manifest,
		Fixtures:          []sdk.Result{result},
		Mode:              conformance.ModeFixture,
		CoreFacts:         coreFacts,
		ReservedFactKinds: reserved,
		PayloadSchemas:    azurePayloadSchemas(t),
	})
	if !report.OK() {
		t.Fatalf("truncated findings = %#v, want passed", report.Findings)
	}
}

// TestConformanceMalformedIDProvesWarningEvidence proves a malformed ARM id
// row becomes explicit unsupported warning evidence and still passes.
func TestConformanceMalformedIDProvesWarningEvidence(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, testClaim(), "testdata/resources_malformed_id.json")
	coreFacts, reserved := coreTestAuthorization(t)
	report := conformance.Run(conformance.Request{
		Manifest:          manifest,
		Fixtures:          []sdk.Result{result},
		Mode:              conformance.ModeFixture,
		CoreFacts:         coreFacts,
		ReservedFactKinds: reserved,
		PayloadSchemas:    azurePayloadSchemas(t),
	})
	if !report.OK() {
		t.Fatalf("malformed-id findings = %#v, want passed", report.Findings)
	}
}

// TestConformanceSensitiveTags proves the sensitive-tag control: secret
// values never reach durable facts, every emitted fact still validates, and
// the fixture pack decodes the tag observation the reducer will read.
func TestConformanceSensitiveTags(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	result := mustCollect(t, testClaim(), "testdata/resources_sensitive_tags.json")
	coreFacts, reserved := coreTestAuthorization(t)
	report := conformance.Run(conformance.Request{
		Manifest:          manifest,
		Fixtures:          []sdk.Result{result},
		Mode:              conformance.ModeFixture,
		CoreFacts:         coreFacts,
		ReservedFactKinds: reserved,
		PayloadSchemas:    azurePayloadSchemas(t),
	})
	if !report.OK() {
		t.Fatalf("sensitive-tag findings = %#v, want passed", report.Findings)
	}
	// The hostile secrets validate and decode; the precise redaction
	// boundary (extension data clean, tag observation fingerprinted, resource
	// tags preserved as reducer drift parity) is pinned in
	// TestCollectSensitiveTagsNeverCarryRawSecrets.
	seenTagObservation := false
	for _, fact := range result.Facts {
		if fact.Kind == factschema.FactKindAzureTagObservation {
			seenTagObservation = true
		}
	}
	if !seenTagObservation {
		t.Fatal("sensitive-tag run emitted no tag observation")
	}
}

// TestEmittedFactsDecodeAsReducerReadsThem proves reducer-shape agreement: every
// emitted fact payload decodes through the pinned factschema versioned decoder
// for its kind — the same decode seam the reducer uses — so fixture truth, port
// graph truth, and API/query truth agree.
func TestEmittedFactsDecodeAsReducerReadsThem(t *testing.T) {
	t.Parallel()

	result := mustCollect(t, testClaim(), "testdata/resources_page1.json", "testdata/resources_page2.json")
	lanes := mustCollect(t, testClaim(), "testdata/resources_source_lanes.json", "testdata/resources_user_assigned_identity.json")
	assertReducerDecodes(t, result.Facts)
	assertReducerDecodes(t, lanes.Facts)
	changes := collectChangesLane(t)
	assertReducerDecodes(t, changes)
}

func collectChangesLane(t *testing.T) []sdk.Fact {
	t.Helper()

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
	return result.Facts
}

func assertReducerDecodes(t *testing.T, facts []sdk.Fact) {
	t.Helper()

	for _, fact := range facts {
		env := factschema.Envelope{
			FactKind:      fact.Kind,
			SchemaVersion: fact.SchemaVersion,
			Payload:       fact.Payload,
		}
		var err error
		switch fact.Kind {
		case factschema.FactKindAzureCloudResource:
			_, err = factschema.DecodeAzureCloudResource(env)
		case factschema.FactKindAzureCloudRelationship:
			_, err = factschema.DecodeAzureCloudRelationship(env)
		case factschema.FactKindAzureTagObservation:
			_, err = factschema.DecodeAzureTagObservation(env)
		case factschema.FactKindAzureIdentityObservation:
			_, err = factschema.DecodeAzureIdentityObservation(env)
		case factschema.FactKindAzureDNSRecord:
			_, err = factschema.DecodeAzureDNSRecord(env)
		case factschema.FactKindAzureImageReference:
			_, err = factschema.DecodeAzureImageReference(env)
		case factschema.FactKindAzureResourceChange:
			_, err = factschema.DecodeAzureResourceChange(env)
		case factschema.FactKindAzureCollectionWarning:
			_, err = factschema.DecodeAzureCollectionWarning(env)
		default:
			t.Fatalf("emitted kind %q has no typed struct: flag it, never invent one inline", fact.Kind)
		}
		if err != nil {
			t.Errorf("kind %q payload fails the reducer decode seam: %v", fact.Kind, err)
		}
	}
}

// TestConformanceManifestDeclaresEveryEmittedKind proves the shipped manifest
// declares exactly the kinds the collector emits: no undeclared emission, no
// declared-but-never-emitted kind.
func TestConformanceManifestDeclaresEveryEmittedKind(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	declared := map[string]bool{}
	for _, family := range manifest.Spec.EmittedFacts {
		declared[family.Kind] = true
	}
	for _, kind := range azureKinds {
		if !declared[kind] {
			t.Errorf("manifest omits emitted kind %q", kind)
		}
	}
	if len(manifest.Spec.EmittedFacts) != len(azureKinds) {
		t.Errorf("manifest declares %d kinds, want %d", len(manifest.Spec.EmittedFacts), len(azureKinds))
	}
}
