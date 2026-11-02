// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resourcegraph/armresourcegraph"
)

// stubTokenCredential supplies an inert token credential so constructor tests
// prove explicit-injection wiring without any live Azure authentication call.
type stubTokenCredential struct{}

func (stubTokenCredential) GetToken(
	context.Context,
	policy.TokenRequestOptions,
) (azcore.AccessToken, error) {
	return azcore.AccessToken{}, nil
}

func TestLiveSDKQueryRequestMapsBoundedOptions(t *testing.T) {
	t.Parallel()

	request := liveSDKQueryRequest(LiveResourceGraphRequest{
		Query:              "Resources | project id",
		Subscriptions:      []string{"sub-1"},
		ManagementGroups:   []string{"mg-1"},
		SkipToken:          "skip-2",
		PageSize:           200,
		AllowPartialScopes: true,
	})
	if request.Query == nil || *request.Query != "Resources | project id" {
		t.Fatalf("query = %#v", request.Query)
	}
	if len(request.Subscriptions) != 1 || *request.Subscriptions[0] != "sub-1" {
		t.Fatalf("subscriptions = %#v, want [sub-1]", request.Subscriptions)
	}
	if len(request.ManagementGroups) != 1 || *request.ManagementGroups[0] != "mg-1" {
		t.Fatalf("management groups = %#v", request.ManagementGroups)
	}
	if request.Options == nil {
		t.Fatal("Options = nil, want bounded options")
	}
	if request.Options.Top == nil || *request.Options.Top != 200 {
		t.Fatalf("Top = %#v, want 200", request.Options.Top)
	}
	if request.Options.SkipToken == nil || *request.Options.SkipToken != "skip-2" {
		t.Fatalf("SkipToken = %#v, want skip-2", request.Options.SkipToken)
	}
	if request.Options.ResultFormat == nil ||
		*request.Options.ResultFormat != armresourcegraph.ResultFormatObjectArray {
		t.Fatalf("ResultFormat = %#v, want objectArray", request.Options.ResultFormat)
	}
	if request.Options.AllowPartialScopes == nil || !*request.Options.AllowPartialScopes {
		t.Fatalf("AllowPartialScopes = %#v, want true", request.Options.AllowPartialScopes)
	}
}

func TestLiveSDKPageConversionKeepsProviderShape(t *testing.T) {
	t.Parallel()

	response := armresourcegraph.QueryResponse{
		TotalRecords:    to.Ptr[int64](2),
		Count:           to.Ptr[int64](1),
		ResultTruncated: to.Ptr(armresourcegraph.ResultTruncatedTrue),
		SkipToken:       to.Ptr("skip-2"),
		Data: []any{
			map[string]any{
				"id":             "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"name":           "vm",
				"type":           "microsoft.compute/virtualmachines",
				"tenantId":       "tenant-marker",
				"subscriptionId": "sub-1",
				"resourceGroup":  "rg",
				"location":       "eastus",
				"tags":           map[string]any{"env": "prod"},
			},
		},
	}
	page, err := liveResourceGraphPageFromSDKResponse(response)
	if err != nil {
		t.Fatalf("liveResourceGraphPageFromSDKResponse() error = %v, want nil", err)
	}
	if page.TotalRecords != 2 || page.Count != 1 || !page.ResultTruncated || page.SkipToken != "skip-2" {
		t.Fatalf("page metadata = %+v", page)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Rows))
	}
	row := page.Rows[0]
	if row.ID == "" || row.Tags["env"] != "prod" {
		t.Fatalf("row = %+v", row)
	}
}

func TestLiveSDKPageConversionRejectsTableData(t *testing.T) {
	t.Parallel()

	_, err := liveResourceGraphPageFromSDKResponse(armresourcegraph.QueryResponse{
		Data: map[string]any{"columns": []any{}, "rows": []any{}},
	})
	if err == nil {
		t.Fatal("error = nil, want objectArray conversion error")
	}
}

func TestLiveSDKPageConversionRejectsOversizedRow(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", maxLiveResourceRowJSONBytes)
	_, err := liveResourceGraphPageFromSDKResponse(armresourcegraph.QueryResponse{
		Data: []any{
			map[string]any{
				"id":         "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"name":       "vm",
				"type":       "microsoft.compute/virtualmachines",
				"properties": map[string]any{"safe": oversized},
			},
		},
	})
	if err == nil {
		t.Fatal("error = nil, want oversized row rejection at the 64KiB live bound")
	}
}

func TestLiveSDKClientRequiresConcreteClient(t *testing.T) {
	t.Parallel()

	client := AzureSDKResourceGraphClient{}
	if _, err := client.QueryResources(context.Background(), LiveResourceGraphRequest{}); err == nil {
		t.Fatal("QueryResources() error = nil, want missing SDK client")
	}
}

func TestLiveSDKFromCredentialRequiresCredential(t *testing.T) {
	t.Parallel()

	if _, err := NewAzureSDKResourceGraphClientFromCredential(nil, nil); err == nil {
		t.Fatal("nil credential built a client, want fail-closed")
	}
	if _, err := NewAzureSDKResourceGraphClientFromCredential(stubTokenCredential{}, nil); err != nil {
		t.Fatalf("explicit credential error = %v, want an injected client", err)
	}
}

func TestClassifyAzureSDKThrottleError(t *testing.T) {
	t.Parallel()

	raw := &http.Response{Header: make(http.Header)}
	raw.Header.Set("Retry-After", "7")
	err := &azcore.ResponseError{StatusCode: http.StatusTooManyRequests, ErrorCode: "RateLimiting", RawResponse: raw}
	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorThrottled {
		t.Fatalf("kind = %q, want throttled", liveErr.kind)
	}
	if liveErr.retryAfter != 7*time.Second {
		t.Fatalf("retryAfter = %v, want the Retry-After-derived 7s", liveErr.retryAfter)
	}
}

func TestClassifyAzureSDKExpiredAuthTokenAsStale(t *testing.T) {
	t.Parallel()

	err := &azcore.ResponseError{StatusCode: http.StatusUnauthorized, ErrorCode: "ExpiredAuthenticationToken"}
	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorTokenExpired {
		t.Fatalf("kind = %q, want token_expired", liveErr.kind)
	}
}

func TestClassifyAzureSDKPermissionHidden(t *testing.T) {
	t.Parallel()

	err := &azcore.ResponseError{StatusCode: http.StatusForbidden, ErrorCode: "AuthorizationFailed"}
	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorPermissionHidden {
		t.Fatalf("kind = %q, want permission_hidden", liveErr.kind)
	}
}

func TestClassifyAzureSDKExpiredSkipTokenAsStale(t *testing.T) {
	t.Parallel()

	err := &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: "InvalidSkipToken"}
	liveErr, ok := classifyLiveProviderError(err)
	if !ok {
		t.Fatal("classifyLiveProviderError() ok = false, want true")
	}
	if liveErr.kind != liveProviderErrorSkipTokenExpired {
		t.Fatalf("kind = %q, want skip_token_expired", liveErr.kind)
	}
}

func TestClassifyUnknownErrorStaysUnclassified(t *testing.T) {
	t.Parallel()

	if _, ok := classifyLiveProviderError(context.DeadlineExceeded); ok {
		t.Fatal("context deadline classified as a live provider error")
	}
	badRequest := &azcore.ResponseError{StatusCode: http.StatusBadRequest, ErrorCode: "InvalidQuery"}
	if _, ok := classifyLiveProviderError(badRequest); ok {
		t.Fatal("non-skip-token bad request classified as a live provider error")
	}
}

func TestRetryAfterFromResponseHonorsShapes(t *testing.T) {
	t.Parallel()

	seconds := &http.Response{Header: make(http.Header)}
	seconds.Header.Set("Retry-After", "7")
	if got := retryAfterFromResponse(seconds); got != 7*time.Second {
		t.Fatalf("seconds Retry-After = %v, want 7s", got)
	}
	past := &http.Response{Header: make(http.Header)}
	past.Header.Set("Retry-After", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat))
	if got := retryAfterFromResponse(past); got != 0 {
		t.Fatalf("past HTTP-date Retry-After = %v, want 0", got)
	}
	if got := retryAfterFromResponse(&http.Response{Header: make(http.Header)}); got != 0 {
		t.Fatalf("missing Retry-After = %v, want 0", got)
	}
	if got := retryAfterFromResponse(nil); got != 0 {
		t.Fatalf("nil response Retry-After = %v, want 0", got)
	}
}
