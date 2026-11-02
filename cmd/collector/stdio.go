// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
)

// resultWithMeta carries one SDK host result plus the proof metadata the run
// loop needs: the success digest and the emitted fact/warning counts.
type resultWithMeta struct {
	result       sdk.Result
	digest       string
	factCount    int
	warningCount int
}

type sdkRequest struct {
	ProtocolVersion string         `json:"protocol_version"`
	Claim           sdk.Claim      `json:"claim"`
	Contract        sdk.Contract   `json:"contract"`
	Config          map[string]any `json:"config,omitempty"`
}

// stdioPlan is a parsed SDK host request: everything parseSDKStdio learns
// from stdin before the wall-time bound starts covering the collection.
// A plan is either fixture-backed (fixture_pages names the page files) or
// live (live opts into the gated seam); it is never both unresolved.
type stdioPlan struct {
	collectorID      string
	targets          []collector.TargetConfig
	fixturePages     []string
	redactionKeyFile string
	live             bool
	limits           collector.ResourceUse
	claim            sdk.Claim
}

// parseSDKStdio decodes and validates one collector SDK host request. It
// performs no collection itself so run() can report the effective limits and
// enforce their wall-time bound around the collect step. Credential material
// never appears here: the redaction key arrives as a FILE PATH, and credential
// references stay names inside the target list.
func parseSDKStdio(stdin io.Reader) (stdioPlan, error) {
	var request sdkRequest
	decoder := json.NewDecoder(stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return stdioPlan{}, fmt.Errorf("decode SDK request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return stdioPlan{}, fmt.Errorf("decode SDK request: trailing JSON value")
		}
		return stdioPlan{}, fmt.Errorf("decode SDK request trailer: %w", err)
	}
	if strings.TrimSpace(request.ProtocolVersion) != sdk.ProtocolVersionV1Alpha1 {
		return stdioPlan{}, fmt.Errorf("protocol_version %q is unsupported", request.ProtocolVersion)
	}
	source, ok := nestedMap(request.Config, "source")
	if !ok {
		return stdioPlan{}, fmt.Errorf("config %q is required", "source")
	}
	targets, err := parseTargets(source["targets"])
	if err != nil {
		return stdioPlan{}, err
	}
	plan := stdioPlan{limits: collector.DefaultResourceUse()}
	if id, ok := source["collector_instance_id"].(string); ok {
		plan.collectorID = strings.TrimSpace(id)
	}
	if live, ok := source["live"].(bool); ok {
		plan.live = live
	}
	// Shared plan fields resolve identically on both paths so the live opt-in
	// honors the same key file, limits, and claim the fixture path uses.
	// Fixture pages are not read on the live path so an opt-in can never
	// silently collect fixtures.
	plan.targets = targets
	if keyFile, ok := source["redaction_key_file"].(string); ok {
		plan.redactionKeyFile = strings.TrimSpace(keyFile)
	}
	if err := applyLimits(&plan.limits, request.Config["limits"]); err != nil {
		return stdioPlan{}, err
	}
	plan.claim = request.Claim
	if plan.live {
		return plan, nil
	}
	pages, err := stringSlice(source["fixture_pages"])
	if err != nil || len(pages) == 0 {
		return stdioPlan{}, fmt.Errorf("config %q is required in fixture mode", "source.fixture_pages")
	}
	plan.fixturePages = pages
	return plan, nil
}

// collectStdioPlan runs one parsed request under the caller's wall-time bound:
// fixture pages are read and collected in fixture mode; live mode reaches the
// gated seam and fails closed until a read-only adapter is injected.
func collectStdioPlan(plan stdioPlan) (resultWithMeta, error) {
	key, err := loadKeyFile(plan.redactionKeyFile)
	if err != nil {
		return resultWithMeta{}, err
	}
	if plan.live {
		// Explicit opt-in reaches the injected live adapter, which requires
		// an ambient credential plus a host claim the satellite authorizes.
		// Anything missing fails closed before any provider call.
		return collectLivePlan(context.Background(), plan, key)
	}
	return collectWithPages(context.Background(), plan.collectorID, plan.targets, plan.claim, plan.limits, plan.fixturePages, key)
}

// collectWithPages builds the fixture provider for the targets' single source
// lane and collects every target authorized by the claim, rendering the SDK
// host result. Mixed-lane page lists are rejected: one ordered fixture page
// list carries exactly one lane, exactly like the in-tree binary.
func collectWithPages(
	ctx context.Context,
	collectorID string,
	targets []collector.TargetConfig,
	claim sdk.Claim,
	limits collector.ResourceUse,
	pages []string,
	key marker.Key,
) (resultWithMeta, error) {
	lane, err := singleFixtureLane(targets)
	if err != nil {
		return resultWithMeta{}, err
	}
	var provider collector.PageProvider
	if lane == collector.SourceLaneResourceChanges {
		provider, err = collector.NewFixtureResourceChangesPageProviderFromFiles(collector.ScopeAccess{}, pages...)
	} else {
		provider, err = collector.NewFixturePageProviderFromFiles(collector.ScopeAccess{}, pages...)
	}
	if err != nil {
		return resultWithMeta{}, err
	}
	return collectWithProvider(ctx, collectorID, targets, claim, limits, provider, key)
}

// singleFixtureLane resolves the one source lane a fixture page list serves.
// Blank lanes default to resource_graph; any other lane is rejected, and mixed
// lanes sharing one page list are rejected.
func singleFixtureLane(targets []collector.TargetConfig) (string, error) {
	lane := ""
	for _, target := range targets {
		candidate := strings.TrimSpace(target.SourceLane)
		if candidate == "" {
			candidate = collector.SourceLaneResourceGraph
		}
		if candidate != collector.SourceLaneResourceGraph && candidate != collector.SourceLaneResourceChanges {
			return "", fmt.Errorf("fixture page provider does not support source lane %q", target.SourceLane)
		}
		if lane == "" {
			lane = candidate
		} else if lane != candidate {
			return "", fmt.Errorf("fixture pages cannot share mixed source lanes %q and %q", lane, candidate)
		}
	}
	if lane == "" {
		lane = collector.SourceLaneResourceGraph
	}
	return lane, nil
}

// collectWithProvider collects every target authorized by the claim through
// the given provider and renders the SDK host result. Facts from all matching
// targets share the claim's generation.
func collectWithProvider(
	ctx context.Context,
	collectorID string,
	targets []collector.TargetConfig,
	claim sdk.Claim,
	limits collector.ResourceUse,
	provider collector.PageProvider,
	key marker.Key,
) (resultWithMeta, error) {
	config := collector.Config{CollectorInstanceID: collectorID, Targets: targets}
	source := &collector.Source{
		Config:          config,
		ProviderFactory: collector.StaticFixtureFactory(provider),
		RedactionKey:    key,
		Clock:           time.Now,
	}
	result, err := source.Collect(ctx, claim)
	if err != nil {
		return resultWithMeta{}, err
	}
	if err := enforceLimits(result, limits); err != nil {
		return resultWithMeta{}, err
	}
	if _, err := sdk.NewValidator(manifestContract()).ValidateResult(result); err != nil {
		return resultWithMeta{}, fmt.Errorf("collected result fails the SDK contract: %w", err)
	}
	return resultWithMeta{
		result:       result,
		digest:       digestOf(result),
		factCount:    len(result.Facts),
		warningCount: countWarnings(result),
	}, nil
}

// runWithTimeout bounds one claim's wall time: a collection that outruns
// limits.ClaimTimeoutSeconds fails the run instead of emitting late evidence.
// Single-shot mode performs no retries; adopters adding a retry loop bound it
// with their own budget.
func runWithTimeout(work func() (resultWithMeta, error), limits collector.ResourceUse) (resultWithMeta, error) {
	type outcome struct {
		result resultWithMeta
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := work()
		done <- outcome{result: result, err: err}
	}()
	timeout := time.Duration(limits.ClaimTimeoutSeconds) * time.Second
	select {
	case finished := <-done:
		return finished.result, finished.err
	case <-time.After(timeout):
		return resultWithMeta{}, fmt.Errorf("claim wall-time bound of %d seconds exceeded", limits.ClaimTimeoutSeconds)
	}
}

// loadKeyFile reads the read-only redaction key material from path. An empty
// path returns a zero key, which disables tag/identity/DNS/image emission. A
// configured-but-unreadable or blank key file is a hard error so the collector
// never silently runs keyless when a key was intended. The material is never
// logged.
func loadKeyFile(path string) (marker.Key, error) {
	if strings.TrimSpace(path) == "" {
		return marker.Key{}, nil
	}
	material, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return marker.Key{}, fmt.Errorf("read redaction key file: %w", err)
	}
	key, err := marker.NewKey(material)
	if err != nil {
		return marker.Key{}, fmt.Errorf("redaction key: %w", err)
	}
	return key, nil
}

func enforceLimits(result sdk.Result, limits collector.ResourceUse) error {
	if limits.MaxRecordsPerClaim > 0 && len(result.Facts) > limits.MaxRecordsPerClaim {
		return fmt.Errorf("claim emitted %d facts past the %d record bound", len(result.Facts), limits.MaxRecordsPerClaim)
	}
	if limits.MaxPayloadBytes > 0 {
		for _, fact := range result.Facts {
			raw, err := json.Marshal(fact.Payload)
			if err != nil {
				return fmt.Errorf("fact payload is not JSON serializable: %w", err)
			}
			if len(raw) > limits.MaxPayloadBytes {
				return fmt.Errorf("fact payload of %d bytes past the %d byte bound", len(raw), limits.MaxPayloadBytes)
			}
		}
	}
	return nil
}

func parseTargets(raw any) ([]collector.TargetConfig, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("config %q requires at least one target", "source.targets")
	}
	targets := make([]collector.TargetConfig, 0, len(items))
	for i, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config source.targets[%d] must be an object", i)
		}
		fencing, err := int64Field(fields, "fencing_token")
		if err != nil {
			return nil, fmt.Errorf("config source.targets[%d]: %w", i, err)
		}
		targets = append(targets, collector.TargetConfig{
			TenantID:           stringField(fields, "tenant_id"),
			ScopeKind:          stringField(fields, "scope_kind"),
			ProviderScopeID:    stringField(fields, "provider_scope_id"),
			ResourceTypeFamily: stringField(fields, "resource_type_family"),
			LocationBucket:     stringField(fields, "location_bucket"),
			CredentialRef:      stringField(fields, "credential_ref"),
			SourceURI:          stringField(fields, "source_uri"),
			SourceLane:         stringField(fields, "source_lane"),
			FencingToken:       fencing,
		})
	}
	return targets, nil
}

func nestedMap(config map[string]any, key string) (map[string]any, bool) {
	raw, ok := config[key]
	if !ok {
		return nil, false
	}
	nested, ok := raw.(map[string]any)
	return nested, ok
}

func stringField(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return strings.TrimSpace(value)
}

func stringSlice(raw any) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("expected a string list")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("expected a string list with no blank entries")
		}
		out = append(out, strings.TrimSpace(value))
	}
	return out, nil
}

func int64Field(fields map[string]any, key string) (int64, error) {
	raw, ok := fields[key]
	if !ok || raw == nil {
		return 0, nil
	}
	switch value := raw.(type) {
	case float64:
		return int64(value), nil
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer", key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func applyLimits(limits *collector.ResourceUse, raw any) error {
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	for key, assign := range map[string]*int{
		"max_records_per_claim": &limits.MaxRecordsPerClaim,
		"max_payload_bytes":     &limits.MaxPayloadBytes,
		"claim_timeout_seconds": &limits.ClaimTimeoutSeconds,
	} {
		value, err := int64Field(fields, key)
		if err != nil {
			return err
		}
		if value < 0 {
			return fmt.Errorf("limits.%s must be non-negative (0 selects defaults)", key)
		}
		if value > 0 {
			*assign = int(value)
		}
	}
	return nil
}

func countWarnings(result sdk.Result) int {
	count := 0
	for _, status := range result.Statuses {
		count += status.WarningCount
	}
	return count
}
