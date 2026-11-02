// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

func mustFixtureProvider(t *testing.T, paths ...string) *FixturePageProvider {
	t.Helper()
	provider, err := NewFixturePageProviderFromFiles(ScopeAccess{}, paths...)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func testTarget() TargetConfig {
	return TargetConfig{
		TenantID:           "tenant-abc",
		ScopeKind:          ScopeKindSubscription,
		ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily: "microsoft.compute",
		LocationBucket:     "eastus",
		CredentialRef:      "azure-read-only-spn",
		FencingToken:       7,
	}
}

func TestCollectParityFixtures(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_page1.json", "testdata/resources_page2.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	boundary := testBoundary()
	result, err := collector.Collect(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceCount != 3 {
		t.Errorf("ResourceCount = %d, want 3 (2 pages, 3 rows)", result.ResourceCount)
	}
	if result.TagObservationCount != 2 {
		t.Errorf("TagObservationCount = %d, want 2 (untagged subnet emits none)", result.TagObservationCount)
	}
	if result.RelationshipCount != 1 {
		t.Errorf("RelationshipCount = %d, want 1 (subnet managed_by)", result.RelationshipCount)
	}
	if result.WarningCount != 0 {
		t.Errorf("WarningCount = %d, want 0", result.WarningCount)
	}
	if result.PageCount != 2 {
		t.Errorf("PageCount = %d, want 2", result.PageCount)
	}
	if result.SkipTokenResumes != 1 {
		t.Errorf("SkipTokenResumes = %d, want 1", result.SkipTokenResumes)
	}
	if calls := provider.Calls(); len(calls) != 2 || calls[0] != "" || calls[1] != "skip-token-page-2" {
		t.Errorf("provider calls = %v, want [\"\" skip-token-page-2]", calls)
	}
	// Deterministic re-emission: the same boundary and provider converge.
	provider2 := mustFixtureProvider(t, "testdata/resources_page1.json", "testdata/resources_page2.json")
	collector2 := NewCollector(provider2, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	again, err := collector2.Collect(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Facts) != len(result.Facts) {
		t.Fatalf("re-emission facts = %d, want %d", len(again.Facts), len(result.Facts))
	}
	for i := range result.Facts {
		if again.Facts[i].StableKey != result.Facts[i].StableKey {
			t.Fatalf("fact %d stable key drifted on re-emission", i)
		}
	}
}

func TestCollectMalformedARMIDIsWarningEvidence(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_malformed_id.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceCount != 1 {
		t.Errorf("ResourceCount = %d, want 1 (malformed row is warning evidence, not a scan failure)", result.ResourceCount)
	}
	if result.WarningCount != 1 {
		t.Errorf("WarningCount = %d, want 1 (unsupported arm id)", result.WarningCount)
	}
	if result.Facts[0].Kind != factschema.FactKindAzureCollectionWarning {
		t.Errorf("first fact Kind = %q, want the malformed-row warning first", result.Facts[0].Kind)
	}
}

func TestCollectTruncatedPageIsWarningEvidence(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_truncated.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	if result.WarningCount != 1 {
		t.Errorf("WarningCount = %d, want 1 (result_truncated)", result.WarningCount)
	}
	last := result.Facts[len(result.Facts)-1]
	if last.Kind != factschema.FactKindAzureCollectionWarning || last.Payload["warning_kind"] != WarningResultTruncated {
		t.Errorf("last fact = %q/%v, want the truncation warning last", last.Kind, last.Payload["warning_kind"])
	}
}

func TestCollectPartialScopeIsWarningEvidence(t *testing.T) {
	t.Parallel()

	access := ScopeAccess{Partial: true, HiddenResourceCount: 4, Reason: WarningPermissionHidden, Message: "some subscriptions hidden"}
	provider, err := NewFixturePageProviderFromFiles(access, "testdata/resources_page2.json")
	if err != nil {
		t.Fatal(err)
	}
	collector := NewCollector(provider, NewCountingMetrics())
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial {
		t.Error("Partial = false, want true")
	}
	last := result.Facts[len(result.Facts)-1]
	if last.Kind != factschema.FactKindAzureCollectionWarning || last.Payload["warning_kind"] != WarningPermissionHidden {
		t.Errorf("last fact = %q/%v, want the permission_hidden warning last", last.Kind, last.Payload["warning_kind"])
	}
}

func TestCollectWithoutRedactionKeyEmitsResourcesOnly(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_page1.json", "testdata/resources_page2.json")
	collector := NewCollector(provider, NewCountingMetrics())
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range result.Facts {
		switch fact.Kind {
		case factschema.FactKindAzureTagObservation,
			factschema.FactKindAzureIdentityObservation,
			factschema.FactKindAzureDNSRecord,
			factschema.FactKindAzureImageReference:
			t.Errorf("keyless run emitted %q, want resource/relationship/warning facts only", fact.Kind)
		}
	}
	if result.TagObservationCount != 0 || result.IdentityObservationCount != 0 {
		t.Error("keyless run counted keyed observations, want zero")
	}
	if result.RelationshipCount != 1 {
		t.Errorf("RelationshipCount = %d, want 1 (managed_by needs no key)", result.RelationshipCount)
	}
}

func TestCollectSensitiveTagsNeverCarryRawSecrets(t *testing.T) {
	t.Parallel()

	provider := mustFixtureProvider(t, "testdata/resources_sensitive_tags.json")
	collector := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t)))
	result, err := collector.Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceCount != 1 || result.TagObservationCount != 1 {
		t.Fatalf("counts = (%d resources, %d tags), want (1, 1)", result.ResourceCount, result.TagObservationCount)
	}
	var resourcePayload map[string]any
	for _, fact := range result.Facts {
		if fact.Kind == factschema.FactKindAzureCloudResource {
			resourcePayload = fact.Payload
		}
	}
	if resourcePayload == nil {
		t.Fatal("no azure_cloud_resource fact emitted")
	}
	extension, _ := resourcePayload["extension"].(map[string]any)
	var redactedKeys []string
	switch keys := extension["redacted_keys"].(type) {
	case []string:
		redactedKeys = keys
	case []any:
		for _, key := range keys {
			if s, ok := key.(string); ok {
				redactedKeys = append(redactedKeys, s)
			}
		}
	}
	// The surviving extension DATA carries no secret keys and no secret
	// values. (The key names resurface under redacted_keys by design, and raw
	// tag values stay on the resource fact as pinned parity below; neither
	// reintroduces credential material.)
	dataRaw, err := json.Marshal(extension["data"])
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "SENSITIVE", "adminPassword", "connectionString", "customData"} {
		if strings.Contains(strings.ToLower(string(dataRaw)), strings.ToLower(secret)) {
			t.Errorf("redacted extension data still carries %q", secret)
		}
	}
	// Managed-identity principal GUIDs never reach durable facts anywhere.
	// (Access-policy object IDs ride the redacted extension as safe
	// control-plane metadata, exactly as the in-tree producer emits them;
	// the extension drop policy, not this test, owns that taxonomy.)
	raw, err := json.Marshal(result.Facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "22222222-2222-2222-2222-222222222222") {
		t.Error("emitted facts carry a raw principal GUID")
	}
	for _, want := range []string{"connectionString", "adminPassword"} {
		found := false
		for _, key := range redactedKeys {
			if key == want {
				found = true
			}
		}
		if !found {
			t.Errorf("extension redacted_keys = %v, want %q recorded", redactedKeys, want)
		}
	}
	// Parity pin: the resource fact preserves the fixture tags verbatim for
	// the reducer's drift evidence. Tag VALUE secrecy on this fact is owned
	// by the core readback strip, not the collector.
	tags, _ := resourcePayload["tags"].(map[string]string)
	if len(tags) == 0 {
		t.Fatal("resource fact dropped the fixture tags, breaking reducer drift parity")
	}
	// The paired tag observation carries fingerprints only, never values.
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureTagObservation {
			continue
		}
		marshaled, err := json.Marshal(fact.Payload["tag_value_fingerprints"])
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"hunter2", "SENSITIVE", "prod"} {
			if strings.Contains(string(marshaled), secret) {
				t.Errorf("tag observation carries raw value %q", secret)
			}
		}
	}
}

func TestSourceNextSweepsConfiguredTargets(t *testing.T) {
	t.Parallel()

	config := Config{CollectorInstanceID: "azure-instance-1", Targets: []TargetConfig{testTarget()}}
	provider := mustFixtureProvider(t, "testdata/resources_page2.json")
	source := &Source{
		Config:          config,
		ProviderFactory: StaticFixtureFactory(provider),
		Metrics:         NewCountingMetrics(),
		Clock:           func() time.Time { return time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC) },
	}
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Next = exhausted, want the configured target")
	}
	if len(collected.Facts) == 0 {
		t.Error("no facts collected for the configured target")
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Errorf("second Next = (%v, %v), want exhaustion", ok, err)
	}
}

func TestSourceRejectsUnconfiguredClaimScope(t *testing.T) {
	t.Parallel()

	config := Config{CollectorInstanceID: "azure-instance-1", Targets: []TargetConfig{testTarget()}}
	source := &Source{Config: config, ProviderFactory: StaticFixtureFactory(mustFixtureProvider(t, "testdata/resources_page2.json"))}
	claim := testClaim()
	claim.Scope.ID = "azure:tenant-abc:subscription:other:all:all:resource_graph"
	if _, err := source.Collect(context.Background(), claim); err == nil {
		t.Error("unconfigured scope collected, want rejection before any provider call")
	}
}

func TestLiveProviderFactoryStaysGated(t *testing.T) {
	t.Parallel()

	factory := LiveProviderFactory{}
	if _, err := factory.PageProvider(context.Background(), testBoundary(), testTarget()); err == nil {
		t.Error("zero-value live factory served a provider, want ErrLiveProviderGated")
	} else if !strings.Contains(err.Error(), "gated") {
		t.Errorf("live factory error = %v, want the gated seam", err)
	}
}

func TestFixtureProviderFromFilesRequiresPages(t *testing.T) {
	t.Parallel()

	if _, err := NewFixturePageProviderFromFiles(ScopeAccess{}); err == nil {
		t.Error("empty page list accepted, want error")
	}
	if _, err := NewFixturePageProviderFromFiles(ScopeAccess{}, "testdata/does-not-exist.json"); err == nil {
		t.Error("missing page file accepted, want error")
	}
	provider, err := NewFixturePageProviderFromFiles(ScopeAccess{}, "testdata/resources_page1.json", "testdata/resources_page2.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.NextPage(context.Background(), "unknown-token"); err == nil {
		t.Error("unknown skip token served, want an explicit gap error")
	}
}
