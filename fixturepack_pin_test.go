// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/conformance"
	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu-sdk/factschema/fixturepack"
)

// pinnedCollectorVersion and pinnedFactschemaVersion record the immutable
// contract pins this satellite builds against. They mirror go.mod; the test
// below keeps the two from drifting so a dependency bump is always deliberate
// and recorded in the handoff.
const (
	pinnedCollectorVersion  = "v0.2.0-rc.3"
	pinnedFactschemaVersion = "v0.1.0-rc.12"
)

// TestPinnedContractVersions proves the module pins the exact immutable SDK
// versions the port was proven against. A bump must update both go.mod and
// these constants together.
func TestPinnedContractVersions(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"github.com/eshu-hq/eshu-sdk/collector " + pinnedCollectorVersion,
		"github.com/eshu-hq/eshu-sdk/factschema " + pinnedFactschemaVersion,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("go.mod lacks the pinned contract %q", want)
		}
	}
}

// TestPinnedFixturePackPassesConformance proves the collector pins the
// fixture-pack version (the factschema module version) and validates payload
// shape against the exact schemas the target reducer release decodes: the
// pack's valid payload passes and its invalid payload fails closed.
func TestPinnedFixturePackPassesConformance(t *testing.T) {
	t.Parallel()

	schema, ok := fixturepack.SchemaFor("azure_cloud_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no schema for azure_cloud_resource")
	}
	schemas := map[string]json.RawMessage{factschema.FactKindAzureCloudResource: schema}

	validPayload, ok := fixturepack.ValidPayload("azure_cloud_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no valid payload for azure_cloud_resource")
	}
	if report := conformance.Run(pinnedRequest(t, validPayload, schemas)); !report.OK() {
		t.Fatalf("pinned valid payload: findings = %#v, want passed", report.Findings)
	}

	invalidPayload, ok := fixturepack.InvalidPayload("azure_cloud_resource")
	if !ok {
		t.Fatal("pinned fixture pack ships no invalid payload for azure_cloud_resource")
	}
	report := conformance.Run(pinnedRequest(t, invalidPayload, schemas))
	if report.OK() {
		t.Fatal("pinned invalid payload: report OK = true, want failed closed")
	}
	if !hasFinding(report, conformance.FindingPayloadSchemaInvalid) {
		t.Fatalf("pinned invalid payload: findings = %#v, want payload_schema_invalid", report.Findings)
	}
}

func pinnedRequest(t *testing.T, payload map[string]any, schemas map[string]json.RawMessage) conformance.Request {
	t.Helper()

	observedAt := testObservedAt()
	manifest := loadManifest(t)
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		State:           sdk.ResultComplete,
		Claim:           testClaim(),
		Generation:      sdk.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []sdk.Fact{{
			Kind:             factschema.FactKindAzureCloudResource,
			SchemaVersion:    "1.0.0",
			StableKey:        "azure:resource:1",
			SourceConfidence: sdk.SourceConfidenceReported,
			ObservedAt:       observedAt,
			SourceRef: sdk.SourceRef{
				SourceSystem: SourceSystem,
				ScopeID:      testClaim().Scope.ID,
				GenerationID: "generation-1",
				FactKey:      "azure:resource:1",
				URI:          "https://management.azure.com/",
				RecordID:     "record-1",
			},
			Payload: payload,
		}},
	}
	coreFacts, reserved := coreTestAuthorization(t)
	return conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture, PayloadSchemas: schemas, CoreFacts: coreFacts, ReservedFactKinds: reserved}
}

func hasFinding(report conformance.Report, code conformance.FindingCode) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
