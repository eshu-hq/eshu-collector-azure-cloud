// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"sort"
	"sync"
)

// Status classes bound the API-call outcome label. They are coarse enums, never
// raw HTTP codes or provider error strings.
const (
	// StatusClassSuccess marks a successful provider read.
	StatusClassSuccess = "success"
	// StatusClassThrottled marks a throttled provider read.
	StatusClassThrottled = "throttled"
	// StatusClassError marks a failed provider read.
	StatusClassError = "error"
)

// Claim status values are bounded enums for the claim lifecycle counter. They
// carry collector kind and status only, never scope, credential, or provider
// identity.
const (
	// ClaimStatusSucceeded marks a claim that committed with full coverage.
	ClaimStatusSucceeded = "succeeded"
	// ClaimStatusPartial marks a claim that committed with durable partial-scope
	// warning evidence.
	ClaimStatusPartial = "partial"
	// ClaimStatusFailed marks a claim that failed before committing complete
	// evidence.
	ClaimStatusFailed = "failed"
)

// Metrics records bounded Azure collector telemetry. Every label is a bounded
// enum (collector kind, scope kind, source lane, operation, status class, fact
// kind, warning reason). Implementations must never accept ARM IDs,
// subscription IDs, tenant IDs, resource group or resource names, locations,
// tags, KQL text, URLs, or credential names as labels.
type Metrics interface {
	// RecordAPICall records one Resource Graph or ARM call by operation and
	// status class.
	RecordAPICall(ctx context.Context, boundary Boundary, operation, statusClass string)
	// RecordSkipTokenResume records one $skipToken continuation fetch.
	RecordSkipTokenResume(ctx context.Context, boundary Boundary)
	// RecordPartialScope records one partial-scope outcome by bounded reason.
	RecordPartialScope(ctx context.Context, boundary Boundary, reason string)
	// RecordFactsEmitted records facts emitted by fact kind.
	RecordFactsEmitted(ctx context.Context, boundary Boundary, factKind string, count int)
	// RecordFreshnessLag records provider-to-Eshu freshness lag in seconds.
	RecordFreshnessLag(ctx context.Context, boundary Boundary, lagSeconds float64)
	// RecordClaim records one claim lifecycle outcome by bounded status enum.
	// It carries collector kind and status only, never scope, credential, or
	// provider identity.
	RecordClaim(ctx context.Context, status string)
}

// NopMetrics is a Metrics that records nothing. It lets callers run the
// collector without a meter.
type NopMetrics struct{}

// RecordAPICall does nothing.
func (NopMetrics) RecordAPICall(context.Context, Boundary, string, string) {}

// RecordSkipTokenResume does nothing.
func (NopMetrics) RecordSkipTokenResume(context.Context, Boundary) {}

// RecordPartialScope does nothing.
func (NopMetrics) RecordPartialScope(context.Context, Boundary, string) {}

// RecordFactsEmitted does nothing.
func (NopMetrics) RecordFactsEmitted(context.Context, Boundary, string, int) {}

// RecordFreshnessLag does nothing.
func (NopMetrics) RecordFreshnessLag(context.Context, Boundary, float64) {}

// RecordClaim does nothing.
func (NopMetrics) RecordClaim(context.Context, string) {}

// CountingMetrics is an in-memory Metrics for fixture runs, local proof, and
// tests. Counters are keyed by bounded-enum label sets only; it accepts no
// identity values and retains none.
type CountingMetrics struct {
	mu     sync.Mutex
	counts map[string]int64
}

// NewCountingMetrics builds an empty CountingMetrics.
func NewCountingMetrics() *CountingMetrics {
	return &CountingMetrics{counts: map[string]int64{}}
}

// RecordAPICall counts one provider call by operation and status class.
func (m *CountingMetrics) RecordAPICall(_ context.Context, boundary Boundary, operation, statusClass string) {
	m.add("api_calls|"+boundary.ScopeKind+"|"+boundary.SourceLane+"|"+operation+"|"+statusClass, 1)
}

// RecordSkipTokenResume counts one $skipToken continuation fetch.
func (m *CountingMetrics) RecordSkipTokenResume(_ context.Context, boundary Boundary) {
	m.add("skip_token_resumes|"+boundary.ScopeKind+"|"+boundary.SourceLane, 1)
}

// RecordPartialScope counts one partial-scope outcome by bounded reason.
func (m *CountingMetrics) RecordPartialScope(_ context.Context, boundary Boundary, reason string) {
	m.add("partial_scope|"+boundary.ScopeKind+"|"+boundary.SourceLane+"|"+reason, 1)
}

// RecordFactsEmitted counts emitted facts by fact kind.
func (m *CountingMetrics) RecordFactsEmitted(_ context.Context, boundary Boundary, factKind string, count int) {
	if count <= 0 {
		return
	}
	m.add("facts_emitted|"+boundary.ScopeKind+"|"+boundary.SourceLane+"|"+factKind, int64(count))
}

// RecordFreshnessLag counts one freshness-lag observation. The in-memory
// counter keeps observability of the call without histogram storage.
func (m *CountingMetrics) RecordFreshnessLag(_ context.Context, boundary Boundary, _ float64) {
	m.add("freshness_lag_observations|"+boundary.ScopeKind+"|"+boundary.SourceLane, 1)
}

// RecordClaim counts one claim lifecycle outcome by bounded status enum.
func (m *CountingMetrics) RecordClaim(_ context.Context, status string) {
	m.add("claims|"+CollectorKind+"|"+status, 1)
}

// Snapshot returns a deterministic copy of the recorded counters.
func (m *CountingMetrics) Snapshot() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.counts))
	for key, value := range m.counts {
		out[key] = value
	}
	return out
}

// Series returns the counter series names in sorted order for stable proof
// output.
func (m *CountingMetrics) Series() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.counts))
	for key := range m.counts {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func (m *CountingMetrics) add(key string, delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[key] += delta
}
