// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
	"github.com/eshu-hq/eshu-sdk/factschema"
)

// stubLiveResourceGraphClient is the hermetic live transport for stdio tests:
// it serves parsed fixture pages through the narrow client interface without
// any network access.
type stubLiveResourceGraphClient struct {
	calls []collector.LiveResourceGraphRequest
	pages []collector.ResourceGraphPage
}

func (m *stubLiveResourceGraphClient) QueryResources(
	_ context.Context,
	request collector.LiveResourceGraphRequest,
) (collector.LiveResourceGraphResponse, error) {
	m.calls = append(m.calls, request)
	if len(m.pages) == 0 {
		return collector.LiveResourceGraphResponse{}, errors.New("unexpected live query")
	}
	page := m.pages[0]
	m.pages = m.pages[1:]
	return collector.LiveResourceGraphResponse{Page: page}, nil
}

func stubLiveFactoryWithPages(t *testing.T, paths ...string) *stubLiveResourceGraphClient {
	t.Helper()
	stub := &stubLiveResourceGraphClient{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		page, err := collector.ParseResourceGraphPage(raw)
		if err != nil {
			t.Fatal(err)
		}
		stub.pages = append(stub.pages, page)
	}
	return stub
}

func liveTestClaim() sdk.Claim {
	return sdk.Claim{
		ComponentID:   collector.ComponentID,
		InstanceID:    "azure-instance-1",
		CollectorKind: collector.CollectorKind,
		SourceSystem:  collector.SourceSystem,
		Scope:         sdk.Scope{ID: "azure:tenant-abc:subscription:11111111-1111-1111-1111-111111111111:microsoft.compute:eastus:resource_graph", Kind: "subscription"},
		SourceRunID:   "source-run-1",
		GenerationID:  "generation-live-1",
		WorkItemID:    "work-item-1",
		FencingToken:  "7",
		Attempt:       1,
		Deadline:      time.Now().UTC().Add(time.Hour),
		ConfigHandle:  "config-1",
	}
}

func liveTestTargets() []collector.TargetConfig {
	return []collector.TargetConfig{{
		TenantID:           "tenant-abc",
		ScopeKind:          collector.ScopeKindSubscription,
		ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
		ResourceTypeFamily: "microsoft.compute",
		LocationBucket:     "eastus",
		CredentialRef:      "azure-read-only-spn",
		SourceURI:          "https://management.azure.com/",
		FencingToken:       7,
	}}
}

// stubLiveProviderFactory replaces the ambient-credential factory for one
// test, restoring the real constructor on cleanup so no test can leak a fake
// into another run.
func stubLiveProviderFactory(t *testing.T, stub *stubLiveResourceGraphClient, called *bool) {
	t.Helper()
	previous := newLiveProviderFactory
	newLiveProviderFactory = func(context.Context, string) (collector.PageProviderFactory, error) {
		if called != nil {
			*called = true
		}
		return collector.LiveProviderFactory{ResourceGraphClient: stub}, nil
	}
	t.Cleanup(func() { newLiveProviderFactory = previous })
}

func TestStdioLiveCollectsThroughInjectedFactory(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	stub := stubLiveFactoryWithPages(t, "../../testdata/resources_page1.json", "../../testdata/resources_page2.json")
	stubLiveProviderFactory(t, stub, nil)
	keyFile := t.TempDir() + "/redaction.key"
	if err := os.WriteFile(keyFile, []byte("live-stdio-test-redaction-key-material"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := loadKeyFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	plan := stdioPlan{
		collectorID:      "azure-instance-1",
		targets:          liveTestTargets(),
		redactionKeyFile: keyFile,
		live:             true,
		limits:           collector.DefaultResourceUse(),
		claim:            liveTestClaim(),
	}
	result, err := collectLivePlan(context.Background(), plan, key)
	if err != nil {
		t.Fatalf("collectLivePlan() error = %v, want nil", err)
	}
	if result.result.Generation.ID != "generation-live-1" {
		t.Fatalf("Generation.ID = %q, want the host claim generation", result.result.Generation.ID)
	}
	if len(result.result.Facts) != 6 {
		t.Fatalf("facts = %d, want 6 (3 resources + 2 tags + 1 relationship)", len(result.result.Facts))
	}
	if len(stub.calls) != 2 {
		t.Fatalf("live calls = %d, want 2 (skip-token resume)", len(stub.calls))
	}
	if stub.calls[1].SkipToken != "skip-token-page-2" {
		t.Fatalf("resume skip token = %q, want the page-1 continuation", stub.calls[1].SkipToken)
	}
}

func TestStdioLiveRejectsUnauthorizedClaimBeforeProviderCall(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	var factoryCalled bool
	stubLiveProviderFactory(t, &stubLiveResourceGraphClient{}, &factoryCalled)
	claim := liveTestClaim()
	claim.InstanceID = "some-other-instance"
	plan := stdioPlan{
		collectorID: "azure-instance-1",
		targets:     liveTestTargets(),
		live:        true,
		limits:      collector.DefaultResourceUse(),
		claim:       claim,
	}
	if _, err := collectLivePlan(context.Background(), plan, marker.Key{}); err == nil {
		t.Fatal("unauthorized claim collected, want rejection before any provider call")
	}
	if factoryCalled {
		t.Fatal("provider factory reached with an unauthorized claim, want claim-first gating")
	}
}

func TestStdioLiveRequiresCredentialRef(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	var factoryCalled bool
	stubLiveProviderFactory(t, &stubLiveResourceGraphClient{}, &factoryCalled)
	targets := liveTestTargets()
	targets[0].CredentialRef = ""
	plan := stdioPlan{
		collectorID: "azure-instance-1",
		targets:     targets,
		live:        true,
		limits:      collector.DefaultResourceUse(),
		claim:       liveTestClaim(),
	}
	_, err := collectLivePlan(context.Background(), plan, marker.Key{})
	if err == nil {
		t.Fatal("unnamed credential collected, want the credential gate")
	} else if !strings.Contains(err.Error(), "credential reference") {
		t.Fatalf("error = %v, want the credential gate", err)
	}
	if factoryCalled {
		t.Fatal("provider factory reached without a credential, want fail-closed")
	}
}

func TestStdioLiveRequiresSingleCredentialRef(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	var factoryCalled bool
	stubLiveProviderFactory(t, &stubLiveResourceGraphClient{}, &factoryCalled)
	targets := liveTestTargets()
	second := targets[0]
	second.ProviderScopeID = "22222222-2222-2222-2222-222222222222"
	second.CredentialRef = "a-different-spn"
	targets = append(targets, second)
	plan := stdioPlan{
		collectorID: "azure-instance-1",
		targets:     targets,
		live:        true,
		limits:      collector.DefaultResourceUse(),
		claim:       liveTestClaim(),
	}
	_, err := collectLivePlan(context.Background(), plan, marker.Key{})
	if err == nil {
		t.Fatal("split credential references collected, want the single-credential gate")
	} else if !strings.Contains(err.Error(), "one credential reference") {
		t.Fatalf("error = %v, want the single-credential gate", err)
	}
	if factoryCalled {
		t.Fatal("provider factory reached with split credentials, want fail-closed")
	}
}

func TestRunLiveStdioMarksNonFixtureHealth(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	stub := stubLiveFactoryWithPages(t, "../../testdata/resources_page1.json", "../../testdata/resources_page2.json")
	stubLiveProviderFactory(t, stub, nil)
	request := map[string]any{
		"protocol_version": sdk.ProtocolVersionV1Alpha1,
		"claim":            liveTestClaim(),
		"contract":         map[string]any{"protocol_version": sdk.ProtocolVersionV1Alpha1},
		"config": map[string]any{
			"source": map[string]any{
				"collector_instance_id": "azure-instance-1",
				"live":                  true,
				"targets": []any{map[string]any{
					"tenant_id": "tenant-abc", "scope_kind": "subscription",
					"provider_scope_id":    "11111111-1111-1111-1111-111111111111",
					"resource_type_family": "microsoft.compute", "location_bucket": "eastus",
					"credential_ref": "azure-read-only-spn", "fencing_token": float64(7),
				}},
			},
		},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--sdk-stdio"}, bytes.NewReader(raw), &stdout, &stderr); err != nil {
		t.Fatalf("run --sdk-stdio live error = %v; stderr = %s", err, stderr.String())
	}
	var result sdk.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not an SDK result: %v", err)
	}
	if result.State != sdk.ResultComplete {
		t.Fatalf("State = %q, want complete", result.State)
	}
	if !strings.Contains(stderr.String(), `"fixture_only":false`) {
		t.Fatalf("stderr health line mislabels the live run: %s", stderr.String())
	}
}

func TestLiveFactKindsStayTyped(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		factschema.FactKindAzureCloudResource,
		factschema.FactKindAzureCollectionWarning,
	} {
		if strings.TrimSpace(kind) == "" {
			t.Fatal("live path references a blank fact kind")
		}
	}
}
