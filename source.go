// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
)

// CollectedGeneration is one bounded Azure scope target's collected source
// evidence. The host commits it under the claim's generation; the satellite
// never commits, schedules claims, or writes graph truth.
type CollectedGeneration struct {
	// ScopeID is the durable scope the facts were collected under.
	ScopeID string
	// GenerationID is the generation the facts belong to.
	GenerationID string
	// Facts are the ordered emitted source facts.
	Facts []sdk.Fact
	// Scan is the bounded scan summary (page, warning, and kind counts).
	Scan ScanResult
}

// Source reads each configured Azure scope target through the PageProvider seam
// and yields one CollectedGeneration per target. The live Resource Graph
// client stays behind ProviderFactory, so the same logic runs under fixtures
// and behind the gated live seam. Source mutates no Azure state and commits no
// facts; the SDK host owns the durable commit boundary.
type Source struct {
	// Config declares the collector instance and bounded scope targets.
	Config Config
	// ProviderFactory builds the read-only PageProvider for each target. A
	// fixture factory is used in fixture mode; LiveProviderFactory is the
	// gated production seam.
	ProviderFactory PageProviderFactory
	// Metrics records bounded-label Azure collector telemetry. A nil value
	// disables Azure-specific metrics.
	Metrics Metrics
	// RedactionKey keys azure_tag_observation value fingerprinting. A zero key
	// (the default) disables tag observation emission entirely, so tag values
	// are never fingerprinted or carried without an operator-provided key.
	RedactionKey marker.Key
	// Clock supplies the observation time. Nil uses time.Now in UTC.
	Clock func() time.Time
	// GenerationIDFunc assigns the generation identity for one target sweep.
	// Nil derives a deterministic fingerprint from scope identity and observed
	// time so a replayed sweep at the same instant converges.
	GenerationIDFunc func(scopeID string, observedAt time.Time) string

	next int
}

// Next returns the next configured Azure scope target's collected generation.
// It advances through Config.Targets one target per call and reports exhaustion
// (ok=false) once the batch is drained, resetting for the next poll. A
// provider read error aborts the sweep instead of committing silently
// incomplete evidence.
func (s *Source) Next(ctx context.Context) (CollectedGeneration, bool, error) {
	config, err := s.Config.validated()
	if err != nil {
		return CollectedGeneration{}, false, err
	}
	if s.ProviderFactory == nil {
		return CollectedGeneration{}, false, fmt.Errorf("azure page provider factory is required")
	}
	if s.next >= len(config.Targets) {
		s.next = 0
		return CollectedGeneration{}, false, nil
	}
	target := config.Targets[s.next]
	s.next++

	collected, err := s.scanTarget(ctx, config.CollectorInstanceID, target, "")
	if err != nil {
		return CollectedGeneration{}, false, err
	}
	return collected, true, nil
}

// Collect gathers the claim-authorized scope target and returns the SDK host
// result for it. The static config authorizes scope targets and credential
// references; the claim supplies the coordinator-assigned generation and
// fencing identity. An unauthorized or stale claim is rejected before any
// provider call, and errors never embed configured provider identity so claim
// rejection telemetry stays bounded.
func (s *Source) Collect(ctx context.Context, claim sdk.Claim) (sdk.Result, error) {
	target, err := s.targetForClaim(claim)
	if err != nil {
		return sdk.Result{}, err
	}
	config, err := s.Config.validated()
	if err != nil {
		return sdk.Result{}, err
	}
	if s.ProviderFactory == nil {
		return sdk.Result{}, fmt.Errorf("azure page provider factory is required")
	}
	observedAt := s.now()
	collected, err := s.scanTarget(ctx, config.CollectorInstanceID, target, strings.TrimSpace(claim.GenerationID))
	if err != nil {
		return sdk.Result{}, err
	}
	return collected.Result(claim, observedAt), nil
}

// AuthorizeClaim validates the host claim against the configured Azure
// collector instance and resolves the single authorized scope target without
// touching the provider. Live wiring calls it before building any adapter so
// an unauthorized or stale claim fails closed with no transport constructed.
func (s *Source) AuthorizeClaim(claim sdk.Claim) (TargetConfig, error) {
	return s.targetForClaim(claim)
}

// targetForClaim validates the host claim against the configured Azure
// collector instance and resolves the single authorized scope target. Errors
// never embed configured provider identity. It needs no provider: Collect and
// Next reject a nil ProviderFactory before they scan, so authorization stays
// callable before any adapter exists.
func (s *Source) targetForClaim(claim sdk.Claim) (TargetConfig, error) {
	config, err := s.Config.validated()
	if err != nil {
		return TargetConfig{}, err
	}
	if strings.TrimSpace(claim.CollectorKind) != CollectorKind {
		return TargetConfig{}, fmt.Errorf("claimed collector_kind %q must be %q", claim.CollectorKind, CollectorKind)
	}
	if strings.TrimSpace(claim.SourceSystem) != SourceSystem {
		return TargetConfig{}, fmt.Errorf("claimed source_system %q must be %q", claim.SourceSystem, SourceSystem)
	}
	if strings.TrimSpace(claim.InstanceID) != config.CollectorInstanceID {
		return TargetConfig{}, fmt.Errorf("claimed instance_id does not match the configured azure collector instance")
	}
	scopeID := strings.TrimSpace(claim.Scope.ID)
	if scopeID == "" {
		return TargetConfig{}, fmt.Errorf("claimed scope id is required")
	}
	if strings.TrimSpace(claim.GenerationID) == "" {
		return TargetConfig{}, fmt.Errorf("claimed generation identity is required")
	}
	if strings.TrimSpace(claim.FencingToken) == "" {
		return TargetConfig{}, fmt.Errorf("claimed fencing token is required")
	}
	for _, candidate := range config.Targets {
		if scopeIDForTarget(candidate) != scopeID {
			continue
		}
		return candidate, nil
	}
	return TargetConfig{}, fmt.Errorf("claimed scope_id is not configured for this collector instance")
}

// scanTarget collects one bounded Azure scope target. A non-empty generationID
// pins the durable generation identity (claim-driven collection supplies the
// coordinator-assigned id); an empty value derives a deterministic per-poll id
// so an unclaimed sweep at the same instant converges.
func (s *Source) scanTarget(
	ctx context.Context,
	collectorInstanceID string,
	target TargetConfig,
	generationID string,
) (CollectedGeneration, error) {
	observedAt := s.now()
	boundary := s.boundary(collectorInstanceID, target, generationID, observedAt)

	provider, err := s.ProviderFactory.PageProvider(ctx, boundary, target)
	if err != nil {
		return CollectedGeneration{}, fmt.Errorf("build azure page provider: %w", err)
	}
	if provider == nil {
		return CollectedGeneration{}, fmt.Errorf("azure page provider factory returned nil provider")
	}

	result, err := NewCollector(provider, s.Metrics, WithRedactionKey(s.RedactionKey)).Collect(ctx, boundary)
	if err != nil {
		s.recordClaim(ctx, ClaimStatusFailed)
		return CollectedGeneration{}, fmt.Errorf("collect azure scope generation: %w", err)
	}

	// Record the coverage-aware claim outcome: partial scope access or any
	// collection warning is a partial claim, not a full success.
	if result.Partial || result.WarningCount > 0 {
		s.recordClaim(ctx, ClaimStatusPartial)
	} else {
		s.recordClaim(ctx, ClaimStatusSucceeded)
	}
	return CollectedGeneration{
		ScopeID:      boundary.ScopeID,
		GenerationID: boundary.GenerationID,
		Facts:        result.Facts,
		Scan:         result,
	}, nil
}

// Result renders one collected generation as the SDK host result for the claim.
// Partial coverage (or any warning fact) yields a partial result with a warning
// status; full coverage yields complete.
func (c CollectedGeneration) Result(claim sdk.Claim, observedAt time.Time) sdk.Result {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	result := sdk.Result{
		ProtocolVersion: sdk.ProtocolVersionV1Alpha1,
		Claim:           claim,
		Generation: sdk.Generation{
			ID:         c.GenerationID,
			ObservedAt: observedAt.UTC(),
		},
		Facts: c.Facts,
	}
	if c.Scan.Partial || c.Scan.WarningCount > 0 {
		result.State = sdk.ResultPartial
		result.Statuses = []sdk.Status{{
			Class:        sdk.StatusWarning,
			Partial:      true,
			WarningCount: c.Scan.WarningCount,
			FactCount:    len(c.Facts),
		}}
		return result
	}
	result.State = sdk.ResultComplete
	result.Statuses = []sdk.Status{{
		Class:     sdk.StatusComplete,
		FactCount: len(c.Facts),
	}}
	return result
}

// recordClaim records one claim lifecycle outcome on the bounded claim counter.
// A nil Metrics disables recording so the source runs without a meter.
func (s *Source) recordClaim(ctx context.Context, status string) {
	if s.Metrics == nil {
		return
	}
	s.Metrics.RecordClaim(ctx, status)
}

func (s *Source) boundary(
	collectorInstanceID string,
	target TargetConfig,
	generationID string,
	observedAt time.Time,
) Boundary {
	scopeID := scopeIDForTarget(target)
	resolvedGenerationID := strings.TrimSpace(generationID)
	if resolvedGenerationID == "" {
		resolvedGenerationID = s.generationID(scopeID, observedAt)
	}
	return Boundary{
		CollectorInstanceID: collectorInstanceID,
		TenantID:            target.TenantID,
		ScopeKind:           target.ScopeKind,
		ProviderScopeID:     target.ProviderScopeID,
		ResourceTypeFamily:  target.ResourceTypeFamily,
		LocationBucket:      target.LocationBucket,
		SourceLane:          target.SourceLane,
		ScopeID:             scopeID,
		GenerationID:        resolvedGenerationID,
		FencingToken:        target.FencingToken,
		ObservedAt:          observedAt,
		SourceURI:           target.SourceURI,
	}
}

func (s *Source) generationID(scopeID string, observedAt time.Time) string {
	if s.GenerationIDFunc != nil {
		if id := strings.TrimSpace(s.GenerationIDFunc(scopeID, observedAt)); id != "" {
			return id
		}
	}
	fingerprint := stableID("AzureCloudGeneration", map[string]any{
		"scope_id":    scopeID,
		"observed_at": observedAt.UTC().Format(time.RFC3339Nano),
	})
	return "azure:" + fingerprint
}

func (s *Source) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
