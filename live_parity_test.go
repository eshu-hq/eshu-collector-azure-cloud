// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// mustParseFixturePage loads one checked-in Resource Graph fixture page so
// parity tests feed the live seam the exact provider shape fixtures use.
func mustParseFixturePage(t *testing.T, path string) ResourceGraphPage {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ParseResourceGraphPage(raw)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// TestLiveTransportRunsUnderPoisonedProxy proves no live-transport test issues
// a network call: the Azure SDK default pipeline honors ProxyFromEnvironment,
// so with every scheme routed to a discard port any real SDK HTTPS attempt
// fails fast and the hermetic fakes (which hold no HTTP client at all) still
// pass. t.Setenv keeps this test non-parallel so the poisoned proxy cannot
// leak into unrelated tests.
func TestLiveTransportRunsUnderPoisonedProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9/")

	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{
		{Page: ResourceGraphPage{
			SkipToken: "resume",
			Rows:      []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}},
		{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(
				"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-2",
				"microsoft.compute/virtualmachines",
				"eastus",
			)},
		}},
	}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() under a poisoned proxy error = %v, want nil (no network call)", err)
	}
	if result.ResourceCount != 2 {
		t.Fatalf("ResourceCount = %d, want 2", result.ResourceCount)
	}
}

// TestLiveCollectionMatchesFixtureParity proves the live seam emits
// byte-identical evidence to the fixture seam for the same provider row: the
// same page collected through the injected live client and through the fixture
// provider must converge on stable keys, kinds, and counts. Any deviation is a
// port defect, not an acceptable difference.
func TestLiveCollectionMatchesFixtureParity(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	first := mustParseFixturePage(t, "testdata/resources_page1.json")
	second := mustParseFixturePage(t, "testdata/resources_page2.json")
	liveProvider, err := LiveProviderFactory{ResourceGraphClient: &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: first}, {Page: second}},
	}}.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	fixtureProvider := mustFixtureProvider(t, "testdata/resources_page1.json", "testdata/resources_page2.json")
	key := testRedactionKey(t)
	liveResult, err := NewCollector(liveProvider, NewCountingMetrics(), WithRedactionKey(key)).Collect(
		context.Background(), testBoundary(),
	)
	if err != nil {
		t.Fatalf("live Collect() error = %v, want nil", err)
	}
	fixtureResult, err := NewCollector(fixtureProvider, NewCountingMetrics(), WithRedactionKey(key)).Collect(
		context.Background(), testBoundary(),
	)
	if err != nil {
		t.Fatalf("fixture Collect() error = %v, want nil", err)
	}
	if len(liveResult.Facts) != len(fixtureResult.Facts) {
		t.Fatalf("live facts = %d, fixture facts = %d, want identical emission", len(liveResult.Facts), len(fixtureResult.Facts))
	}
	for i := range liveResult.Facts {
		if liveResult.Facts[i].StableKey != fixtureResult.Facts[i].StableKey {
			t.Fatalf("fact %d stable key diverged: live %q vs fixture %q",
				i, liveResult.Facts[i].StableKey, fixtureResult.Facts[i].StableKey)
		}
		if liveResult.Facts[i].Kind != fixtureResult.Facts[i].Kind {
			t.Fatalf("fact %d kind diverged: live %q vs fixture %q",
				i, liveResult.Facts[i].Kind, fixtureResult.Facts[i].Kind)
		}
	}
}

// TestLiveCredentialNameNeverSurfaces proves credential hygiene on the live
// path: the credential reference is a name the adapter resolves out of band,
// so no fact payload, warning message, error, or metric series may carry it.
func TestLiveCredentialNameNeverSurfaces(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")

	const canary = "canary-credential-9z9z"
	target := testTarget()
	target.CredentialRef = canary
	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
		Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
	}}}}
	metrics := NewCountingMetrics()
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), target)
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, metrics, WithRedactionKey(testRedactionKey(t))).Collect(
		context.Background(), testBoundary(),
	)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	raw, err := json.Marshal(result.Facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), canary) {
		t.Fatal("credential reference leaked into live fact payloads")
	}
	for _, series := range metrics.Series() {
		if strings.Contains(series, canary) {
			t.Fatalf("credential reference leaked into metric series %q", series)
		}
	}
	// Rejection paths are equally bounded: a lane error for a canary-bearing
	// target must not echo the name.
	badTarget := target
	badTarget.SourceLane = SourceLaneResourceChanges
	if _, err := factory.PageProvider(context.Background(), testBoundary(), badTarget); err == nil {
		t.Fatal("invalid lane served, want fail-closed")
	} else if strings.Contains(err.Error(), canary) {
		t.Fatalf("credential reference leaked into rejection error: %v", err)
	}
}
