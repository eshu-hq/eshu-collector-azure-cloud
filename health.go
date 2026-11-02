// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"sync"
	"time"
)

// Health is the operator-visible liveness snapshot for one collector run. Every
// field is a bounded enum, count, digest, or timestamp: it never carries ARM
// IDs, subscription/tenant IDs, names, tags, KQL, URLs, or credential names.
type Health struct {
	// FixtureOnly marks a run that read fixtures only and issued no live Azure
	// call.
	FixtureOnly bool `json:"fixture_only"`
	// LastSuccessAt is the last time a collection completed, if any.
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	// Digest is the opaque success digest (stable-key hash) of the last good
	// collection, if any.
	Digest string `json:"digest,omitempty"`
	// Failure carries the bounded failure class of the last failed run, if any.
	Failure string `json:"failure,omitempty"`
	// Facts is the fact count of the last run.
	Facts int `json:"facts"`
	// Warnings is the warning-fact count of the last run.
	Warnings int `json:"warnings"`
}

// ResourceUse bounds one collection run: record, payload-byte, and wall-time
// budgets. A run that outgrows a budget fails instead of emitting late or
// unbounded evidence.
type ResourceUse struct {
	// MaxRecordsPerClaim bounds facts per claim (0 selects the default bound).
	MaxRecordsPerClaim int `json:"max_records_per_claim"`
	// MaxPayloadBytes bounds payload bytes per fact (0 selects the default).
	MaxPayloadBytes int `json:"max_payload_bytes"`
	// ClaimTimeoutSeconds bounds one claim's wall time (0 selects the default).
	ClaimTimeoutSeconds int `json:"claim_timeout_seconds"`
}

// DefaultResourceUse returns the default run budgets.
func DefaultResourceUse() ResourceUse {
	return ResourceUse{
		MaxRecordsPerClaim:  10000,
		MaxPayloadBytes:     1048576,
		ClaimTimeoutSeconds: 300,
	}
}

// Monitor is the operator signal behind the run health line: every collection
// outcome is observed, and the snapshot carries liveness, freshness, failure,
// and retry state.
type Monitor struct {
	mu       sync.Mutex
	resource ResourceUse
	health   Health
}

// NewMonitor builds a Monitor with default run budgets.
func NewMonitor() *Monitor {
	return &Monitor{resource: DefaultResourceUse()}
}

// SetResource records the effective limits for the mode in use.
func (m *Monitor) SetResource(limits ResourceUse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resource = limits
}

// ObserveSuccess records a completed collection with its opaque digest.
func (m *Monitor) ObserveSuccess(digest string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.LastSuccessAt = &at
	m.health.Digest = digest
	m.health.Failure = ""
}

// ObserveFailure records a failed run under its bounded failure class.
func (m *Monitor) ObserveFailure(class string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.Failure = class
}

// ObserveCounts records the fact and warning counts of the last run.
func (m *Monitor) ObserveCounts(facts, warnings int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.Facts = facts
	m.health.Warnings = warnings
}

// MarkFixtureOnly labels the run as fixture-only proof.
func (m *Monitor) MarkFixtureOnly() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.FixtureOnly = true
}

// Snapshot returns the current health and run budgets.
func (m *Monitor) Snapshot() (Health, ResourceUse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.health, m.resource
}
