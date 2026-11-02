// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
)

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

// sdkBackedLiveResourceGraphClient is the hermetic live transport stub that
// serves raw Azure SDK QueryResponse values — the objectArray Data shape the
// SDK decodes — and runs every page through the production
// liveResourceGraphPageFromSDKResponse conversion. Parity tests built on it
// prove the real SDK Data-to-rows path instead of bypassing the conversion
// with pre-parsed pages. It holds no HTTP client, so it issues no network
// calls.
type sdkBackedLiveResourceGraphClient struct {
	calls     []LiveResourceGraphRequest
	responses []armresourcegraph.QueryResponse
	errs      []error
}

func (m *sdkBackedLiveResourceGraphClient) QueryResources(
	_ context.Context,
	request LiveResourceGraphRequest,
) (LiveResourceGraphResponse, error) {
	m.calls = append(m.calls, request)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		if err != nil {
			return LiveResourceGraphResponse{}, err
		}
	}
	if len(m.responses) == 0 {
		return LiveResourceGraphResponse{}, errors.New("unexpected live SDK query")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	page, err := liveResourceGraphPageFromSDKResponse(response)
	if err != nil {
		return LiveResourceGraphResponse{}, err
	}
	return LiveResourceGraphResponse{Page: page}, nil
}

// mustSDKFixtureResponse parses one checked-in fixture page file into the raw
// SDK QueryResponse shape the live seam consumes: the file's data array
// becomes the objectArray Data decoded through encoding/json exactly as the
// SDK unmarshals it, with the file's totalRecords, count, resultTruncated,
// and $skipToken carried as the response metadata fields.
func mustSDKFixtureResponse(t *testing.T, path string) armresourcegraph.QueryResponse {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		TotalRecords    int64           `json:"totalRecords"`
		Count           int64           `json:"count"`
		ResultTruncated string          `json:"resultTruncated"`
		SkipToken       string          `json:"$skipToken"`
		Data            json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	var data any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatal(err)
	}
	truncated := armresourcegraph.ResultTruncatedFalse
	if strings.EqualFold(strings.TrimSpace(envelope.ResultTruncated), "true") {
		truncated = armresourcegraph.ResultTruncatedTrue
	}
	return armresourcegraph.QueryResponse{
		TotalRecords:    to.Ptr(envelope.TotalRecords),
		Count:           to.Ptr(envelope.Count),
		ResultTruncated: to.Ptr(truncated),
		SkipToken:       to.Ptr(envelope.SkipToken),
		Data:            data,
	}
}

// TestLiveCollectionMatchesFixtureParity proves the live seam emits
// byte-identical evidence to the fixture seam for the same provider rows: the
// same checked-in fixture files feed the live client as raw SDK QueryResponse
// values (run through the production SDK Data-to-rows conversion) and feed
// the fixture provider as parsed pages, and every emitted fact must converge
// byte-for-byte, in order. Any deviation is a port defect, not an acceptable
// difference.
func TestLiveCollectionMatchesFixtureParity(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9/")

	liveProvider, err := LiveProviderFactory{ResourceGraphClient: &sdkBackedLiveResourceGraphClient{
		responses: []armresourcegraph.QueryResponse{
			mustSDKFixtureResponse(t, "testdata/resources_page1.json"),
			mustSDKFixtureResponse(t, "testdata/resources_page2.json"),
		},
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
		liveRaw, err := json.Marshal(liveResult.Facts[i])
		if err != nil {
			t.Fatal(err)
		}
		fixtureRaw, err := json.Marshal(fixtureResult.Facts[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(liveRaw) != string(fixtureRaw) {
			t.Fatalf("fact %d diverged (live stable key %q, fixture stable key %q):\nlive:    %s\nfixture: %s",
				i, liveResult.Facts[i].StableKey, fixtureResult.Facts[i].StableKey, liveRaw, fixtureRaw)
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
