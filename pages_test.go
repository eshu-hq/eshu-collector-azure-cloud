// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"os"
	"testing"
	"time"
)

func testBoundary() Boundary {
	return Boundary{
		CollectorInstanceID: "azure-instance-1",
		TenantID:            "tenant-abc",
		ScopeKind:           ScopeKindSubscription,
		ProviderScopeID:     "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily:  "microsoft.compute",
		LocationBucket:      "eastus",
		SourceLane:          SourceLaneResourceGraph,
		ScopeID:             "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph",
		GenerationID:        "generation-1",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
		SourceURI:           "https://management.azure.com/",
	}
}

func TestParseResourceGraphPageChainsSkipToken(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/resources_page1.json")
	if err != nil {
		t.Fatal(err)
	}
	page, err := ParseResourceGraphPage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalRecords != 3 || page.Count != 2 {
		t.Errorf("totals = (%d, %d), want (3, 2)", page.TotalRecords, page.Count)
	}
	if page.ResultTruncated {
		t.Error("ResultTruncated = true, want false")
	}
	if page.SkipToken != "skip-token-page-2" {
		t.Errorf("SkipToken = %q, want continuation", page.SkipToken)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(page.Rows))
	}
	if got := page.Rows[0].SKUClass(); got != "Standard" && got != "Standard_D2s_v3" {
		t.Errorf("SKUClass = %q, want the bounded tier or name", got)
	}
	if page.Rows[0].ProviderTime() == nil {
		t.Error("ProviderTime = nil, want the row timestamp")
	}
}

func TestParseResourceGraphPageRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	if _, err := ParseResourceGraphPage([]byte("{not json")); err == nil {
		t.Error("malformed JSON parsed, want error")
	}
}

func TestParseResourceChangesPageKeepsProvenanceOnly(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"totalRecords":1,"count":1,"resultTruncated":"false","$skipToken":"","data":[{
		"id":"change-1",
		"properties":{
			"changeType":"Update",
			"targetResourceId":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-web-01",
			"targetResourceType":"microsoft.compute/virtualmachines",
			"changeAttributes":{"timestamp":"2026-06-09T11:40:00Z","operation":"Microsoft.Compute/virtualMachines/write","clientType":"portal","changedBy":"user@example.invalid","changedByType":"user"},
			"changes":{"properties.hardwareProfile.vmSize":{"propertyName":"properties.hardwareProfile.vmSize"}}
		}}]}`)
	page, err := ParseResourceChangesPage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(page.Changes))
	}
	row := page.Changes[0]
	if row.ChangeType() != ChangeTypeUpdated {
		t.Errorf("ChangeType = %q, want updated", row.ChangeType())
	}
	if row.ChangeTime() == nil {
		t.Error("ChangeTime = nil, want the provider timestamp")
	}
	paths := row.ChangedPropertyPaths()
	if len(paths) != 1 || paths[0] != "properties.hardwareProfile.vmSize" {
		t.Errorf("ChangedPropertyPaths = %v, want the single path", paths)
	}
	observation, err := row.toObservation(testBoundary())
	if err != nil {
		t.Fatal(err)
	}
	if observation.ActorID != "user@example.invalid" {
		t.Errorf("ActorID = %q", observation.ActorID)
	}
}
