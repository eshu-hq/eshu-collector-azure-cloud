// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
)

// liveWarningPayload returns the first azure_collection_warning payload so
// error-class tests assert the exact warning contract the collector emits.
func liveWarningPayload(t *testing.T, result ScanResult) map[string]any {
	t.Helper()
	for _, fact := range result.Facts {
		if fact.Kind != factschema.FactKindAzureCollectionWarning {
			continue
		}
		return fact.Payload
	}
	t.Fatal("no azure_collection_warning fact emitted")
	return nil
}

func TestLiveProviderSkipTokenExpiryYieldsStalePartial(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{
		errs: []error{
			nil,
			liveProviderError{kind: liveProviderErrorSkipTokenExpired},
		},
		responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{
			SkipToken: "expired-token",
			Rows:      []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 1 || result.WarningCount != 1 || !result.Partial {
		t.Fatalf("counts = resources:%d warnings:%d partial:%v, want 1/1/true",
			result.ResourceCount, result.WarningCount, result.Partial)
	}
	payload := liveWarningPayload(t, result)
	if payload["warning_kind"] != WarningStale {
		t.Fatalf("warning_kind = %v, want stale token warning", payload["warning_kind"])
	}
	if payload["retryable"] != true {
		t.Fatalf("retryable = %v, want true", payload["retryable"])
	}
}

func TestLiveProviderTokenExpiryYieldsStalePartial(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{
		errs: []error{liveProviderError{kind: liveProviderErrorTokenExpired}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if result.ResourceCount != 0 || result.WarningCount != 1 || !result.Partial {
		t.Fatalf("counts = resources:%d warnings:%d partial:%v, want 0/1/true",
			result.ResourceCount, result.WarningCount, result.Partial)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningStale {
		t.Fatalf("warning_kind = %v, want stale token warning", payload["warning_kind"])
	}
}

func TestLiveProviderPermissionHiddenYieldsPartialScope(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{
		errs: []error{liveProviderError{kind: liveProviderErrorPermissionHidden}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if !result.Partial || result.WarningCount != 1 {
		t.Fatalf("partial=%v warnings=%d, want a partial permission warning", result.Partial, result.WarningCount)
	}
	payload := liveWarningPayload(t, result)
	if payload["warning_kind"] != WarningPermissionHidden {
		t.Fatalf("warning_kind = %v, want permission_hidden", payload["warning_kind"])
	}
}

func TestLiveProviderHiddenResourceCountSurvivesAccessReport(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{{Page: ResourceGraphPage{}, Access: ScopeAccess{
		Partial:             true,
		HiddenResourceCount: 2,
		Reason:              WarningPermissionHidden,
		Message:             "some configured resources were hidden from the read-only identity",
	}}}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	payload := liveWarningPayload(t, result)
	if payload["hidden_resource_count"] != 2 {
		t.Fatalf("hidden_resource_count = %#v, want 2", payload["hidden_resource_count"])
	}
}

func TestLiveProviderUnsupportedFamilyYieldsWarning(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{
		errs: []error{liveProviderError{kind: liveProviderErrorUnsupported}},
	}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if !result.Partial || result.WarningCount != 1 {
		t.Fatalf("partial=%v warnings=%d, want a partial unsupported warning", result.Partial, result.WarningCount)
	}
	if payload := liveWarningPayload(t, result); payload["warning_kind"] != WarningUnsupported {
		t.Fatalf("warning_kind = %v, want unsupported", payload["warning_kind"])
	}
}

func TestLiveProviderUnknownErrorAbortsScan(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{errs: []error{errors.New("boom")}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	if _, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary()); err == nil {
		t.Fatal("unknown transport error collected success, want an aborted scan")
	}
}

func TestLiveProviderMergesPartialAccessAcrossPages(t *testing.T) {
	t.Parallel()

	client := &fakeLiveResourceGraphClient{responses: []LiveResourceGraphResponse{
		{Page: ResourceGraphPage{
			SkipToken: "resume",
			Rows:      []ResourceRow{liveTestResourceRow(liveTestVMID, "microsoft.compute/virtualmachines", "eastus")},
		}, Access: ScopeAccess{
			Partial:             true,
			HiddenResourceCount: 1,
			Reason:              WarningPermissionHidden,
			Message:             "first page hid one resource",
		}},
		{Page: ResourceGraphPage{}, Access: ScopeAccess{
			Partial: true,
			Reason:  WarningThrottled,
			Message: "second page throttled",
		}},
	}}
	factory := LiveProviderFactory{ResourceGraphClient: client}
	provider, err := factory.PageProvider(context.Background(), testBoundary(), testTarget())
	if err != nil {
		t.Fatalf("PageProvider() error = %v, want nil", err)
	}
	result, err := NewCollector(provider, NewCountingMetrics()).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if !result.Partial || result.WarningCount != 1 {
		t.Fatalf("partial=%v warnings=%d, want merged partial evidence", result.Partial, result.WarningCount)
	}
	access := provider.ScopeAccess(context.Background())
	if !access.Partial || access.HiddenResourceCount != 1 {
		t.Fatalf("merged access = %+v, want partial with hidden count 1", access)
	}
	// Equal-priority reasons keep the first report: permission_hidden wins over
	// the later throttled report instead of flapping between pages.
	if access.Reason != WarningPermissionHidden {
		t.Fatalf("merged reason = %q, want permission_hidden", access.Reason)
	}
}
