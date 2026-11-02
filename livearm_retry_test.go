// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

func TestLiveProviderARMFallbackRetryRecoversAfterPerCallTimeout(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{
		errs:      []error{context.DeadlineExceeded},
		responses: []LiveARMFallbackResponse{{Extension: map[string]any{"powerState": "running"}}},
	}
	var slept []time.Duration
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		MaxRetries:          2,
		BackoffCap:          time.Second,
		Sleep: func(context.Context, time.Duration) error {
			slept = append(slept, time.Second)
			return nil
		},
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics(), WithRedactionKey(testRedactionKey(t))).Collect(
		context.Background(), testBoundary(),
	)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(armFallback.calls) != 2 {
		t.Fatalf("arm fallback calls = %d, want 2 (timeout then recovery)", len(armFallback.calls))
	}
	if len(slept) != 1 {
		t.Fatalf("backoff sleeps = %d, want 1", len(slept))
	}
	if result.Partial {
		t.Fatal("Partial = true, want a clean recovery with no partial evidence")
	}
	var resource map[string]any
	for _, fact := range result.Facts {
		if fact.Kind == factschema.FactKindAzureCloudResource {
			resource = fact.Payload
		}
	}
	if resource == nil {
		t.Fatal("no azure_cloud_resource fact emitted")
	}
	data, ok := resource["extension"].(map[string]any)["data"].(map[string]any)
	if !ok {
		t.Fatalf("extension data = %#v, want the redacted data object", resource["extension"])
	}
	fallback, ok := data["armFallback"].(map[string]any)
	if !ok {
		t.Fatalf("armFallback = %#v, want the bounded fallback wrapper after recovery", data["armFallback"])
	}
	if fallback["data"].(map[string]any)["powerState"] != "running" {
		t.Fatalf("powerState = %v, want running", fallback["data"])
	}
}

func TestLiveProviderARMFallbackTimeoutExhaustionMarksStale(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{
		errs: []error{context.DeadlineExceeded, context.DeadlineExceeded, context.DeadlineExceeded},
	}
	var slept []time.Duration
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		MaxRetries:          2,
		BackoffCap:          time.Second,
		Sleep: func(context.Context, time.Duration) error {
			slept = append(slept, time.Second)
			return nil
		},
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil (exhaustion stays fail-partial)", err)
	}
	if len(armFallback.calls) != 3 {
		t.Fatalf("arm fallback calls = %d, want 3 (initial plus two bounded retries)", len(armFallback.calls))
	}
	if len(slept) != 2 {
		t.Fatalf("backoff sleeps = %d, want 2", len(slept))
	}
	if !result.Partial {
		t.Fatal("Partial = false, want stale-access evidence after retry exhaustion")
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningStale {
		t.Fatalf("warning_kind = %v, want %v", payload["warning_kind"], WarningStale)
	}
}
