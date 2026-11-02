// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeLiveResourceGraphClient is the hermetic live transport stub: it serves
// queued pages and errors without any network access, so every live-provider
// test proves behavior without issuing a live Azure call.
type fakeLiveResourceGraphClient struct {
	calls     []LiveResourceGraphRequest
	responses []LiveResourceGraphResponse
	errs      []error
}

func (m *fakeLiveResourceGraphClient) QueryResources(
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
		return LiveResourceGraphResponse{}, errors.New("unexpected live query")
	}
	response := m.responses[0]
	m.responses = m.responses[1:]
	return response, nil
}

func liveTestResourceRow(id, resourceType, location string) ResourceRow {
	return ResourceRow{
		ID:             id,
		Name:           "vm-1",
		Type:           resourceType,
		TenantID:       "tenant-abc",
		SubscriptionID: "11111111-1111-1111-1111-111111111111",
		ResourceGroup:  "rg-app",
		Location:       location,
		Tags:           map[string]string{"env": "prod"},
	}
}

const liveTestVMID = "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-1"

func TestSourceAuthorizeClaimNeedsNoProvider(t *testing.T) {
	t.Parallel()

	source := &Source{
		Config: Config{CollectorInstanceID: "azure-instance-1", Targets: []TargetConfig{testTarget()}},
		Clock:  testObservedAt,
	}
	if _, err := source.AuthorizeClaim(testClaim()); err != nil {
		t.Fatalf("AuthorizeClaim() error = %v, want nil without any provider built", err)
	}
	bad := testClaim()
	bad.InstanceID = "some-other-instance"
	if _, err := source.AuthorizeClaim(bad); err == nil {
		t.Fatal("unauthorized claim authorized, want fail-closed before any adapter exists")
	}
}

func TestLiveProviderRequiresCredentialRef(t *testing.T) {
	t.Parallel()

	target := testTarget()
	target.CredentialRef = ""
	factory := LiveProviderFactory{ResourceGraphClient: &fakeLiveResourceGraphClient{}}
	if _, err := factory.PageProvider(context.Background(), testBoundary(), target); err == nil {
		t.Fatal("missing credential reference served a provider, want fail-closed")
	}
}

func TestLiveProviderRejectsNonResourceGraphLane(t *testing.T) {
	t.Parallel()

	target := testTarget()
	target.SourceLane = SourceLaneResourceChanges
	factory := LiveProviderFactory{ResourceGraphClient: &fakeLiveResourceGraphClient{}}
	if _, err := factory.PageProvider(context.Background(), testBoundary(), target); err == nil {
		t.Fatal("resource_changes lane served a live provider, want fail-closed")
	}
}

func TestLiveProviderPaginatesBySkipToken(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{
		{Page: ResourceGraphPage{
			TotalRecords: 2,
			Count:        1,
			SkipToken:    "skip-token-page-2",
			Rows:         []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}},
		{Page: ResourceGraphPage{
			TotalRecords: 2,
			Count:        1,
			Rows: []ResourceRow{liveTestResourceRow(
				"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-app/providers/Microsoft.Compute/virtualMachines/vm-2",
				"microsoft.compute/virtualmachines",
				"eastus",
			)},
		}},
	}}
	factory := LiveProviderFactory{ResourceGraphClient: client, PageSize: 200}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 2 {
		t.Fatalf("ResourceCount = %d, want 2", result.ResourceCount)
	}
	if result.PageCount != 2 || result.SkipTokenResumes != 1 {
		t.Fatalf("pages = %d resumes = %d, want 2/1", result.PageCount, result.SkipTokenResumes)
	}
	if len(client.calls) != 2 {
		t.Fatalf("live calls = %d, want 2", len(client.calls))
	}
	if client.calls[0].SkipToken != "" || client.calls[1].SkipToken != "skip-token-page-2" {
		t.Fatalf("skip tokens = %q/%q, want empty then resume", client.calls[0].SkipToken, client.calls[1].SkipToken)
	}
	for _, call := range client.calls {
		if call.PageSize != 200 {
			t.Fatalf("PageSize = %d, want 200", call.PageSize)
		}
		if len(call.Subscriptions) != 1 || call.Subscriptions[0] != testTarget().ProviderScopeID {
			t.Fatalf("subscriptions = %#v, want the target provider scope", call.Subscriptions)
		}
		if strings.TrimSpace(call.Query) == "" {
			t.Fatal("live query is blank")
		}
		if strings.Contains(strings.ToLower(call.Query), "properties") {
			t.Fatalf("default live query projects the properties bag: %q", call.Query)
		}
		if !call.AllowPartialScopes {
			t.Fatal("AllowPartialScopes = false, want bounded partial-scope reads")
		}
	}
}

func TestLiveProviderThrottleBackoffIsBounded(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{
		errs: []error{
			liveProviderError{kind: liveProviderErrorThrottled, retryAfter: 10 * time.Second},
			nil,
		},
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			Rows: []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	var slept []time.Duration
	factory := LiveProviderFactory{
		ResourceGraphClient: client,
		MaxRetries:          1,
		BackoffCap:          2 * time.Second,
		Sleep: func(context.Context, time.Duration) error {
			slept = append(slept, 2*time.Second)
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
	if result.ResourceCount != 1 || result.WarningCount != 1 {
		t.Fatalf("counts = resources:%d warnings:%d, want 1/1", result.ResourceCount, result.WarningCount)
	}
	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Fatalf("sleep calls = %v, want one capped 2s backoff", slept)
	}
	payload := liveWarningPayload(t, result)
	if payload["warning_kind"] != WarningThrottled {
		t.Fatalf("warning_kind = %v, want throttled", payload["warning_kind"])
	}
	if payload["retryable"] != true {
		t.Fatalf("retryable = %v, want true", payload["retryable"])
	}
}

func TestLiveProviderThrottleBudgetExhaustedStaysPartial(t *testing.T) {
	t.Parallel()

	throttled := liveProviderError{kind: liveProviderErrorThrottled, retryAfter: time.Second}
	client := &fakeLiveResourceGraphClient{errs: []error{throttled, throttled, throttled}}
	factory := LiveProviderFactory{
		ResourceGraphClient: client,
		MaxRetries:          2,
		BackoffCap:          time.Millisecond,
		Sleep:               func(context.Context, time.Duration) error { return nil },
	}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(client.calls) != 3 {
		t.Fatalf("live calls = %d, want exactly the 1+2 retry budget", len(client.calls))
	}
	if result.ResourceCount != 0 || result.WarningCount != 1 || !result.Partial {
		t.Fatalf("exhausted throttle = resources:%d warnings:%d partial:%v, want 0/1/true",
			result.ResourceCount, result.WarningCount, result.Partial)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningThrottled {
		t.Fatalf("warning_kind = %v, want throttled", payload["warning_kind"])
	}
}

func TestLiveProviderPageSizeQuotaCapsAt1000(t *testing.T) {
	t.Parallel()

	for _, pageSize := range []int32{0, -5, 5000} {
		client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{}}}}
		factory := LiveProviderFactory{ResourceGraphClient: client, PageSize: pageSize}
		provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
		if err != nil {
			t.Fatalf("PageSize %d: PageProvider() error = %v, want nil", pageSize, err)
		}
		if _, err := provider.NextPage(context.Background(), ""); err != nil {
			t.Fatalf("PageSize %d: NextPage() error = %v, want nil", pageSize, err)
		}
		if got := client.calls[0].PageSize; got != 1000 {
			t.Fatalf("PageSize %d normalized to %d, want the 1000 quota cap/default", pageSize, got)
		}
	}
}

func TestLiveProviderRequestTimeoutBoundsStuckQuery(t *testing.T) {
	t.Parallel()

	stuck := &blockingLiveClient{}
	factory := LiveProviderFactory{ResourceGraphClient: stuck, RequestTimeout: 20 * time.Millisecond}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	start := time.Now()
	if _, err := provider.NextPage(context.Background(), ""); err == nil {
		t.Fatal("stuck query returned success, want the timeout bound")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("stuck query ran %v, want the timeout bound", elapsed)
	}
}

// blockingLiveClient honors context cancellation instead of answering, so the
// timeout test proves the request bound without flaky sleeps.
type blockingLiveClient struct{}

func (blockingLiveClient) QueryResources(
	ctx context.Context,
	_ LiveResourceGraphRequest,
) (LiveResourceGraphResponse, error) {
	<-ctx.Done()
	return LiveResourceGraphResponse{}, ctx.Err()
}

func TestLiveProviderManagementGroupScopeSelectsGroups(t *testing.T) {
	t.Parallel()

	target := testTarget()
	target.ScopeKind = ScopeKindManagementGroup
	target.ProviderScopeID = "mg-1"
	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{}}}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), target)
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	if _, err := provider.NextPage(context.Background(), ""); err != nil {
		t.Fatalf("NextPage() error = %v, want nil", err)
	}
	call := client.calls[0]
	if len(call.ManagementGroups) != 1 || call.ManagementGroups[0] != "mg-1" {
		t.Fatalf("management groups = %#v, want [mg-1]", call.ManagementGroups)
	}
	if len(call.Subscriptions) != 0 {
		t.Fatalf("subscriptions = %#v, want none on the management-group lane", call.Subscriptions)
	}
}
