// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// TestLocalScopeMatchesLibraryLayout pins the scope layout the local runner
// derives to the library's own ScopeIDForTarget: the proof claim and the
// collector must authorize the same scope string.
func TestLocalScopeMatchesLibraryLayout(t *testing.T) {
	t.Parallel()

	target := collector.TargetConfig{
		TenantID:           "tenant-abc",
		ScopeKind:          collector.ScopeKindSubscription,
		ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily: "Microsoft.Compute",
		LocationBucket:     "EastUS",
	}
	scopeID, err := collector.ScopeIDForTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	want := "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph"
	if scopeID != want {
		t.Errorf("ScopeIDForTarget = %q, want %q", scopeID, want)
	}
	claim := fixtureClaim("azure-instance-1", []collector.TargetConfig{target}, "generation-1")
	if claim.Scope.ID != want {
		t.Errorf("fixture claim scope = %q, want %q", claim.Scope.ID, want)
	}
}

func TestSDKStdioRoundTrip(t *testing.T) {
	t.Parallel()

	claim := sdk.Claim{
		ComponentID:   collector.ComponentID,
		InstanceID:    "azure-instance-1",
		CollectorKind: collector.CollectorKind,
		SourceSystem:  collector.SourceSystem,
		Scope:         sdk.Scope{ID: "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph", Kind: "subscription"},
		SourceRunID:   "source-run-1",
		GenerationID:  "generation-stdio-1",
		WorkItemID:    "work-item-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Now().UTC().Add(time.Hour),
		ConfigHandle:  "config-1",
	}
	keyFile := t.TempDir() + "/redaction.key"
	if err := os.WriteFile(keyFile, []byte("stdio-test-redaction-key-material"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"protocol_version": sdk.ProtocolVersionV1Alpha1,
		"claim":            claim,
		"contract":         map[string]any{"protocol_version": sdk.ProtocolVersionV1Alpha1},
		"config": map[string]any{
			"source": map[string]any{
				"collector_instance_id": "azure-instance-1",
				"redaction_key_file":    keyFile,
				"targets": []any{map[string]any{
					"tenant_id": "tenant-abc", "scope_kind": "subscription",
					"provider_scope_id":    "11111111-1111-1111-1111-111111111111",
					"resource_type_family": "microsoft.compute", "location_bucket": "eastus",
					"credential_ref": "azure-read-only-spn", "fencing_token": float64(7),
				}},
				"fixture_pages": []any{"../../testdata/resources_page1.json", "../../testdata/resources_page2.json"},
			},
		},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, bytes.NewReader(raw), &stdout, &stderr); err != nil {
		t.Fatalf("run --sdk-stdio error = %v; stderr = %s", err, stderr.String())
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not an SDK result: %v", err)
	}
	if result.State != sdk.ResultComplete {
		t.Errorf("State = %q, want complete", result.State)
	}
	if len(result.Facts) != 6 {
		t.Errorf("facts = %d, want 6 (3 resources + 2 tags + 1 relationship)", len(result.Facts))
	}
	if result.Generation.ID != "generation-stdio-1" {
		t.Errorf("Generation.ID = %q, want the claim generation", result.Generation.ID)
	}
	if !strings.Contains(stderr.String(), `"fixture_only":true`) {
		t.Errorf("stderr health line lacks the fixture_only label: %s", stderr.String())
	}
}

func TestSDKStdioRejectsBadProtocolAndLive(t *testing.T) {
	t.Parallel()

	if _, err := parseSDKStdio(strings.NewReader(`{"protocol_version":"nope"}`)); err == nil {
		t.Error("unsupported protocol accepted, want error")
	}
	if _, err := parseSDKStdio(strings.NewReader(`{"protocol_version":"collector-sdk/v1alpha1"}`)); err == nil {
		t.Error("missing source config accepted, want error")
	}
	live := `{"protocol_version":"collector-sdk/v1alpha1","claim":{},"contract":{},
		"config":{"source":{"collector_instance_id":"x","targets":[{"tenant_id":"t","scope_kind":"subscription","provider_scope_id":"s"}],"live":true}}}`
	plan, err := parseSDKStdio(strings.NewReader(live))
	if err != nil {
		t.Fatalf("live opt-in must parse, gating happens at collect: %v", err)
	}
	if !plan.live {
		t.Error("live opt-in lost, want the gated seam")
	}
	if _, err := collectStdioPlan(plan); err == nil {
		t.Error("live collection served without an adapter, want ErrLiveProviderGated")
	} else if !strings.Contains(err.Error(), "gated") {
		t.Errorf("live error = %v, want the gated seam", err)
	}
}

func TestLocalModeRejectsMultiTarget(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	targets := `[{"tenant_id":"t","scope_kind":"subscription","provider_scope_id":"s"},{"tenant_id":"t","scope_kind":"tenant","provider_scope_id":"s"}]`
	err := run([]string{"--pages", "../../testdata/resources_page2.json", "--targets", targets}, nil, &stdout, &stderr)
	if err == nil {
		t.Error("multi-target local run accepted, want exactly-one-target")
	}
}

func TestLocalModeLiveFlagFailsClosed(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"--live"}, nil, &stdout, &stderr)
	if err == nil {
		t.Error("--live served, want ErrLiveProviderGated")
	} else if !strings.Contains(err.Error(), "gated") {
		t.Errorf("--live error = %v, want the gated seam", err)
	}
}
