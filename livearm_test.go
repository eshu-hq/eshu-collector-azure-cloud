// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

// fakeLiveARMFallbackClient is the hermetic ARM fallback stub: it serves
// queued extensions and errors without any network access.
type fakeLiveARMFallbackClient struct {
	calls     []LiveARMFallbackRequest
	responses []LiveARMFallbackResponse
	errs      []error
}

func (m *fakeLiveARMFallbackClient) GetResource(
	_ context.Context,
	request LiveARMFallbackRequest,
) (LiveARMFallbackResponse, error) {
	m.calls = append(m.calls, request)
	if len(m.errs) > 0 {
		err := m.errs[0]
		m.errs = m.errs[1:]
		if err != nil {
			return LiveARMFallbackResponse{}, err
		}
	}
	if len(m.responses) == 0 {
		return LiveARMFallbackResponse{}, errors.New("unexpected arm fallback query")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func liveARMRule() LiveARMFallbackRule {
	return LiveARMFallbackRule{
		ResourceType:    "microsoft.compute/virtualmachines",
		APIVersion:      "2024-03-01",
		ExtensionFields: []string{"powerState", "administratorPassword", "privateIPAddress"},
	}
}

func TestLiveProviderARMFallbackEnrichesAllowlistedRows(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{
		responses: []LiveARMFallbackResponse{{Extension: map[string]any{
			"powerState":              "running",
			"administratorPassword":   "must-not-persist",
			"privateIPAddress":        "must-not-persist",
			"unrequestedSafeMetadata": "must-not-persist",
		}}},
	}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
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
	if result.ResourceCount != 1 || result.WarningCount != 0 {
		t.Fatalf("counts = resources:%d warnings:%d, want 1/0", result.ResourceCount, result.WarningCount)
	}
	if len(armFallback.calls) != 1 {
		t.Fatalf("arm fallback calls = %d, want 1", len(armFallback.calls))
	}
	call := armFallback.calls[0]
	if call.ResourceID != liveTestVMID || call.APIVersion != "2024-03-01" {
		t.Fatalf("fallback call = %+v, want the resource id and allowlisted API version", call)
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
	extension, ok := resource["extension"].(map[string]any)
	if !ok {
		t.Fatalf("extension = %#v, want the redacted extension object", resource["extension"])
	}
	data, ok := extension["data"].(map[string]any)
	if !ok {
		t.Fatalf("extension data = %#v, want the redacted data object", extension["data"])
	}
	fallback, ok := data["armFallback"].(map[string]any)
	if !ok {
		t.Fatalf("armFallback = %#v, want the bounded fallback wrapper", data["armFallback"])
	}
	if fallback["schema_version"] != liveARMFallbackExtensionSchemaVersion {
		t.Fatalf("arm fallback schema = %v, want the pinned wrapper version", fallback["schema_version"])
	}
	fallbackData, ok := fallback["data"].(map[string]any)
	if !ok {
		t.Fatalf("arm fallback data = %#v, want the allowlisted fields", fallback["data"])
	}
	if fallbackData["powerState"] != "running" {
		t.Fatalf("powerState = %v, want running", fallbackData["powerState"])
	}
	for _, forbidden := range []string{"administratorPassword", "privateIPAddress", "unrequestedSafeMetadata"} {
		if _, ok := fallbackData[forbidden]; ok {
			t.Fatalf("forbidden/unrequested fallback field %q persisted: %#v", forbidden, fallbackData)
		}
	}
	raw, err := json.Marshal(result.Facts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "must-not-persist") {
		t.Fatal("fallback secret leaked into emitted facts")
	}
}

func TestLiveProviderARMFallbackRejectsIncompleteOptIn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		factory LiveProviderFactory
	}{
		{
			name: "allowlist without client",
			factory: LiveProviderFactory{
				ResourceGraphClient: &fakeLiveResourceGraphClient{},
				ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
			},
		},
		{
			name: "client without allowlist",
			factory: LiveProviderFactory{
				ResourceGraphClient: &fakeLiveResourceGraphClient{},
				ARMFallbackClient:   &fakeLiveARMFallbackClient{},
			},
		},
		{
			name: "duplicate rule",
			factory: LiveProviderFactory{
				ResourceGraphClient: &fakeLiveResourceGraphClient{},
				ARMFallbackClient:   &fakeLiveARMFallbackClient{},
				ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule(), liveARMRule()},
			},
		},
		{
			name: "rule without api version",
			factory: LiveProviderFactory{
				ResourceGraphClient: &fakeLiveResourceGraphClient{},
				ARMFallbackClient:   &fakeLiveARMFallbackClient{},
				ARMFallbackRules: []LiveARMFallbackRule{{
					ResourceType:    "microsoft.compute/virtualmachines",
					ExtensionFields: []string{"powerState"},
				}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := tc.factory.PageProvider(context.Background(), testBoundary(), testTarget()); err == nil {
				t.Fatal("PageProvider() error = nil, want fail-closed ARM fallback configuration")
			}
		})
	}
}

func TestLiveProviderARMFallbackSkippedForUnallowlistedRows(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(
				"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Network/publicIPAddresses/pip-1",
				"microsoft.network/publicipaddresses",
				"eastus",
			)},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(armFallback.calls) != 0 {
		t.Fatalf("arm fallback calls = %d, want none for a non-allowlisted family", len(armFallback.calls))
	}
	if !result.Partial || result.WarningCount != 1 {
		t.Fatalf("partial=%v warnings=%d, want fallback-skipped evidence", result.Partial, result.WarningCount)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningFallbackSkipped {
		t.Fatalf("warning_kind = %v, want fallback_skipped", payload["warning_kind"])
	}
}

func TestLiveProviderARMFallbackErrorMapsToPartialAccess(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{
		errs: []error{liveProviderError{kind: liveProviderErrorThrottled}},
	}
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		// No-op sleep: throttled fallback errors now retry with capped
		// backoff, and this test asserts warning evidence, not the delay.
		Sleep: func(context.Context, time.Duration) error { return nil },
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 1 || !result.Partial {
		t.Fatalf("counts = resources:%d partial:%v, want the resource with partial fallback evidence",
			result.ResourceCount, result.Partial)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningThrottled {
		t.Fatalf("warning_kind = %v, want throttled fallback evidence", payload["warning_kind"])
	}
}

func TestLiveProviderARMFallbackCapStopsCallsAndMarksPartial(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9/")

	const extra = 5
	total := maxLiveARMFallbackCallsPerPage + extra
	rows := make([]ResourceRow, 0, total)
	responses := make([]LiveARMFallbackResponse, 0, total)
	for i := 0; i < total; i++ {
		rows = append(rows, liveTestResourceRow(
			fmt.Sprintf("/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-%03d", i),
			"microsoft.compute/virtualmachines",
			"eastus",
		))
		responses = append(responses, LiveARMFallbackResponse{Extension: map[string]any{"powerState": "running"}})
	}
	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{Rows: rows}}},
	}
	armFallback := &fakeLiveARMFallbackClient{responses: responses}
	metrics := NewCountingMetrics()
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		Metrics:             metrics,
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
	if len(armFallback.calls) != maxLiveARMFallbackCallsPerPage {
		t.Fatalf("arm fallback calls = %d, want the %d per-page cap", len(armFallback.calls), maxLiveARMFallbackCallsPerPage)
	}
	if result.ResourceCount != total || !result.Partial {
		t.Fatalf("counts = resources:%d partial:%v, want %d resources with partial cap evidence",
			result.ResourceCount, result.Partial, total)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningFallbackCapped {
		t.Fatalf("warning_kind = %v, want fallback_capped", payload["warning_kind"])
	}
	series := metrics.Series()
	found := false
	for _, name := range series {
		if strings.Contains(name, "arm_fallback_get") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no arm_fallback_get accounting series, want per-attempt call evidence in %v", series)
	}
}

func TestLiveProviderARMFallbackRetryFiresOnlyOnRetryableClasses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		err         error
		wantCalls   int
		wantWarning string
		wantSleeps  int
	}{
		{name: "throttled retries once then recovers", err: liveProviderError{kind: liveProviderErrorThrottled}, wantCalls: 2, wantWarning: WarningThrottled, wantSleeps: 1},
		{name: "permission hidden never retries", err: liveProviderError{kind: liveProviderErrorPermissionHidden}, wantCalls: 1, wantWarning: WarningPermissionHidden},
		{name: "skip token expiry never retries", err: liveProviderError{kind: liveProviderErrorSkipTokenExpired}, wantCalls: 1, wantWarning: WarningStale},
		{name: "token expiry never retries", err: liveProviderError{kind: liveProviderErrorTokenExpired}, wantCalls: 1, wantWarning: WarningStale},
		{name: "unsupported never retries", err: liveProviderError{kind: liveProviderErrorUnsupported}, wantCalls: 1, wantWarning: WarningUnsupported},
		{name: "unknown error never retries", err: errors.New("boom"), wantCalls: 1, wantWarning: WarningUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resourceGraph := &fakeLiveResourceGraphClient{
				responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
					Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
				}}},
			}
			armFallback := &fakeLiveARMFallbackClient{
				errs:      []error{tc.err},
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
			result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
			if err != nil {
				t.Fatalf("Collect() error = %v, want nil", err)
			}
			if len(armFallback.calls) != tc.wantCalls {
				t.Fatalf("arm fallback calls = %d, want %d", len(armFallback.calls), tc.wantCalls)
			}
			if len(slept) != tc.wantSleeps {
				t.Fatalf("backoff sleeps = %d, want %d", len(slept), tc.wantSleeps)
			}
			if !result.Partial {
				t.Fatal("Partial = false, want fallback evidence to stay fail-partial")
			}
			if payload := liveWarningPayload(t, result); payload["warning_kind"] != tc.wantWarning {
				t.Fatalf("warning_kind = %v, want %v", payload["warning_kind"], tc.wantWarning)
			}
		})
	}
}

func TestLiveProviderARMFallbackAttemptsAreCounted(t *testing.T) {
	t.Parallel()

	resourceGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	armFallback := &fakeLiveARMFallbackClient{
		responses: []LiveARMFallbackResponse{{Extension: map[string]any{"powerState": "running"}}},
	}
	metrics := NewCountingMetrics()
	factory := LiveProviderFactory{
		ResourceGraphClient: resourceGraph,
		ARMFallbackClient:   armFallback,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		Metrics:             metrics,
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	if _, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary()); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	snapshot := metrics.Snapshot()
	if got := snapshot["api_calls|subscription|arm_fallback|arm_fallback_get|success"]; got != 1 {
		t.Fatalf("success call count = %d, want 1 counted attempt in %v", got, snapshot)
	}

	throttled := liveProviderError{kind: liveProviderErrorThrottled}
	exhaustedGraph := &fakeLiveResourceGraphClient{
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	exhaustedARM := &fakeLiveARMFallbackClient{errs: []error{throttled, throttled, throttled}}
	exhaustedMetrics := NewCountingMetrics()
	exhaustedFactory := LiveProviderFactory{
		ResourceGraphClient: exhaustedGraph,
		ARMFallbackClient:   exhaustedARM,
		ARMFallbackRules:    []LiveARMFallbackRule{liveARMRule()},
		MaxRetries:          2,
		BackoffCap:          time.Millisecond,
		Sleep:               func(context.Context, time.Duration) error { return nil },
		Metrics:             exhaustedMetrics,
	}
	exhaustedProvider, err := exhaustedFactory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	if _, err := NewCollector(exhaustedProvider, NewCountingMetrics()).Collect(context.Background(), testBoundary()); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	exhausted := exhaustedMetrics.Snapshot()
	if got := exhausted["api_calls|subscription|arm_fallback|arm_fallback_get|throttled"]; got != 3 {
		t.Fatalf("throttled call count = %d, want 3 (1 initial + 2 retries) in %v", got, exhausted)
	}
}

func TestLiveARMClientRequiresConcreteClient(t *testing.T) {
	t.Parallel()

	client := AzureSDKARMFallbackClient{}
	request := LiveARMFallbackRequest{ResourceID: liveTestVMID, ResourceType: "microsoft.compute/virtualmachines", APIVersion: "2024-03-01"}
	if _, err := client.GetResource(context.Background(), request); err == nil {
		t.Fatal("GetResource() error = nil, want missing SDK client")
	}
}

func TestLiveARMFromCredentialValidatesInputs(t *testing.T) {
	t.Parallel()

	if _, err := NewAzureSDKARMFallbackClientFromCredential("", stubTokenCredential{}, nil); err == nil {
		t.Fatal("blank subscription built a client, want fail-closed")
	}
	if _, err := NewAzureSDKARMFallbackClientFromCredential("sub-1", nil, nil); err == nil {
		t.Fatal("nil credential built a client, want fail-closed")
	}
	if _, err := NewAzureSDKARMFallbackClientFromCredential("sub-1", stubTokenCredential{}, nil); err != nil {
		t.Fatalf("explicit credential error = %v, want an injected client", err)
	}
}
