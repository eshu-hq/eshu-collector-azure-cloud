// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"fmt"
	"strings"
	"time"
)

// ComponentID is the durable producer identity this satellite emits under. It
// matches manifest.yaml and grant.example.json.
const ComponentID = "dev.eshu.collector.azure-cloud"

// CollectorKind is the durable collector_kind value for Azure cloud facts.
const CollectorKind = "azure"

// SourceSystem is the durable source_system value carried on every fact
// source reference.
const SourceSystem = "azure"

// ComponentVersion is the satellite release version. It matches
// manifest.yaml metadata.version and the grant example version.
const ComponentVersion = "0.1.0"

// Scope kinds bound the Azure shard hierarchy. They are durable enum values
// used in scope identity and bounded telemetry labels, never raw IDs.
const (
	// ScopeKindSubscription bounds a claim to one Azure subscription.
	ScopeKindSubscription = "subscription"
	// ScopeKindManagementGroup bounds a claim to one Azure management group.
	ScopeKindManagementGroup = "management_group"
	// ScopeKindTenant bounds a claim to a tenant-level scope.
	ScopeKindTenant = "tenant"
)

// Source lanes bound the provider read path for one observation. They are
// enum values safe for telemetry labels.
const (
	// SourceLaneResourceGraph marks evidence from the Resource Graph Resources
	// API.
	SourceLaneResourceGraph = "resource_graph"
	// SourceLaneResourceChanges marks evidence from Resource Graph
	// resourcechanges (fixture-only in this satellite).
	SourceLaneResourceChanges = "resource_changes"
	// SourceLaneARMFallback marks evidence from an allowlisted, read-only ARM
	// fallback GET. The satellite serves this lane from fixtures only; live
	// fallback stays gated with the live provider seam.
	SourceLaneARMFallback = "arm_fallback"
)

// Azure DNS record types supported by source-lane extraction.
const (
	DNSRecordTypeA     = "A"
	DNSRecordTypeAAAA  = "AAAA"
	DNSRecordTypeCNAME = "CNAME"
	DNSRecordTypeMX    = "MX"
	DNSRecordTypeNS    = "NS"
	DNSRecordTypePTR   = "PTR"
	DNSRecordTypeTXT   = "TXT"
	DNSRecordTypeSRV   = "SRV"
	DNSRecordTypeCAA   = "CAA"
)

// Warning kinds bound the azure_collection_warning evidence taxonomy.
const (
	// WarningPartialScope marks a scope where only part of the configured
	// subscriptions or management groups were readable.
	WarningPartialScope = "partial_scope"
	// WarningPermissionHidden marks subscriptions or resources hidden from the
	// principal by Azure RBAC.
	WarningPermissionHidden = "permission_hidden"
	// WarningResultTruncated marks a Resource Graph response that set
	// resultTruncated.
	WarningResultTruncated = "result_truncated"
	// WarningThrottled marks a scope where Resource Graph quota throttling
	// forced backoff.
	WarningThrottled = "throttled"
	// WarningFallbackSkipped marks an ARM fallback skipped because the resource
	// family was not allowlisted.
	WarningFallbackSkipped = "fallback_skipped"
	// WarningStale marks evidence older than the configured freshness window.
	WarningStale = "stale"
	// WarningUnsupported marks a resource family or evidence type the source
	// does not expose.
	WarningUnsupported = "unsupported"
	// WarningRedaction marks a payload where extension fields were redacted.
	WarningRedaction = "redaction"
)

// Outcomes bound the warning resolution state.
const (
	// OutcomePartial means the scope produced partial evidence.
	OutcomePartial = "partial"
	// OutcomeStale means the scope produced stale evidence.
	OutcomeStale = "stale"
	// OutcomeUnavailable means the scope produced no current evidence.
	OutcomeUnavailable = "unavailable"
	// OutcomeUnsupported means the evidence type is not exposed.
	OutcomeUnsupported = "unsupported"
)

// Boundary carries the durable scope-generation and claim identity shared by
// every fact emitted for one Azure collection claim. It encodes the scope and
// generation contract fields the Azure collector contract requires.
type Boundary struct {
	// CollectorInstanceID is the configured runtime instance that owns target
	// policy and the credential environment for the claim.
	CollectorInstanceID string
	// TenantID is the Azure tenant ID (or tenant fingerprint) for the shard.
	TenantID string
	// ScopeKind is one of ScopeKindSubscription, ScopeKindManagementGroup, or
	// ScopeKindTenant.
	ScopeKind string
	// ProviderScopeID is the subscription ID, management group ID, or tenant
	// fingerprint kept as source evidence.
	ProviderScopeID string
	// ResourceTypeFamily buckets the resource provider namespace for sharding,
	// for example "microsoft.compute".
	ResourceTypeFamily string
	// LocationBucket buckets the Azure location for sharding, for example
	// "eastus".
	LocationBucket string
	// SourceLane is one of the SourceLane* enum values.
	SourceLane string
	// SourceURI is the bounded Resource Graph source URI for evidence. Empty
	// selects the default Resource Graph endpoint so every emitted fact carries
	// a contract-valid source reference.
	SourceURI string
	// ScopeID is the durable Eshu scope for the Azure shard.
	ScopeID string
	// GenerationID is the collector- or coordinator-assigned generation for one
	// bounded scan.
	GenerationID string
	// FencingToken fences the durable claim. It must be positive.
	FencingToken int64
	// ObservedAt is the Eshu observation time for the provider response.
	ObservedAt time.Time
}

// ResourceGraphEndpoint is the default bounded Resource Graph source URI. It
// carries no credentials, query, or host-local path, so it satisfies the SDK
// source-reference contract for fixture runs that declare no source URI.
const ResourceGraphEndpoint = "https://management.azure.com/"

// ResourceObservation describes one Azure Resource Graph resource row after
// ARM identity normalization. RawExtension carries safe control-plane metadata
// that is redacted before emission; it must never carry object contents,
// secrets, private payloads, or data-plane records.
type ResourceObservation struct {
	Boundary Boundary
	// ARMResourceID is the raw provider identity preserved for exact reducer
	// joins.
	ARMResourceID string
	// Identity is the normalized identity parsed from the ARM resource ID.
	Identity ARMIdentity
	// Kind is the optional Azure resource kind discriminator.
	Kind string
	// SKUClass is the bounded SKU class (tier or name) when present.
	SKUClass string
	// ManagedBy is the ARM ID of an owning resource when present.
	ManagedBy string
	// APIVersion is the provider API version when known.
	APIVersion string
	// ProviderTime is the Resource Graph read or change time. Nil means the
	// provider did not report a time and the absence is explicit.
	ProviderTime *time.Time
	// Tags carries Resource Graph tags within the configured evidence retention
	// boundary. Sensitive tag values are redacted by the envelope builder.
	Tags map[string]string
	// HasIdentity reports whether the resource exposes a managed identity.
	HasIdentity bool
	// RawExtension is the provider-specific control-plane metadata object. It is
	// redacted before persistence.
	RawExtension map[string]any
	// SourceRecordID overrides the default Resource Graph record id.
	SourceRecordID string
	// SourceURI is the bounded Resource Graph source URI.
	SourceURI string
}

// WarningObservation describes one explicit Azure collection coverage outcome:
// partial scope, permission-hidden subscription, truncation, throttling,
// fallback skip, stale, unsupported, or redaction warning.
type WarningObservation struct {
	Boundary Boundary
	// WarningKind is a bounded enum from the Warning* constants.
	WarningKind string
	// Outcome is a bounded enum from the Outcome* constants.
	Outcome string
	// Retryable reports whether the condition may resolve on retry.
	Retryable bool
	// HiddenResourceCount counts resources the principal could not read in the
	// scope, when known.
	HiddenResourceCount int
	// Message is an operator-facing detail. It is sanitized before persistence.
	Message string
	// SourceRecordID overrides the default warning record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// ScopeAccess reports partial-scope coverage for one claim. Partial true means
// the principal could read only part of the configured subscriptions or
// management groups, which the collector surfaces as explicit warning evidence
// rather than silent success.
type ScopeAccess struct {
	// Partial reports incomplete scope coverage.
	Partial bool
	// HiddenResourceCount counts resources hidden from the principal, when
	// known.
	HiddenResourceCount int
	// Reason is a bounded Warning* enum describing why coverage is partial.
	Reason string
	// Message is an operator-facing detail, sanitized before persistence.
	Message string
}

// TargetConfig describes one bounded Azure scope shard: a tenant plus a
// subscription, management group, or tenant-level scope, optionally narrowed by
// resource type family and location bucket. CredentialRef names the read-only
// credential the live adapter would use; it is a NAME only and never a secret
// value, so configuration stays safe to log and persist.
type TargetConfig struct {
	// TenantID is the Azure tenant ID (or tenant fingerprint) for the shard.
	TenantID string
	// ScopeKind is one of ScopeKindSubscription, ScopeKindManagementGroup, or
	// ScopeKindTenant.
	ScopeKind string
	// ProviderScopeID is the subscription ID, management group ID, or tenant
	// fingerprint kept as source evidence.
	ProviderScopeID string
	// ResourceTypeFamily buckets the resource provider namespace for sharding.
	// It may be empty for a whole-scope sweep.
	ResourceTypeFamily string
	// LocationBucket buckets the Azure location for sharding. It may be empty
	// for a cross-location sweep.
	LocationBucket string
	// CredentialRef names the read-only credential the live Resource
	// Graph/ARM adapter would use. It is a name, never a secret value.
	CredentialRef string
	// SourceURI is the bounded Resource Graph source URI for evidence.
	SourceURI string
	// SourceLane selects the bounded provider read lane. Blank defaults to
	// SourceLaneResourceGraph; resource_changes is fixture-only.
	SourceLane string
	// FencingToken fences the durable generation. When zero the config assigns
	// the default positive token so emitted facts stay contract-valid.
	FencingToken int64
}

// defaultFencingToken is the positive fencing token assigned to a target that
// does not declare one. The non-claimed runtime owns generation identity, so a
// stable positive token keeps emitted facts contract-valid.
const defaultFencingToken = 1

// Config describes one Azure cloud collector satellite instance. It is fully
// declarative: every scope target and credential reference is named, never an
// inline secret.
type Config struct {
	// CollectorInstanceID is the configured satellite instance that owns target
	// policy and the credential environment for the configured scopes.
	CollectorInstanceID string
	// Targets are the bounded Azure scope shards this instance reads.
	Targets []TargetConfig
}

// Validate reports whether the declarative config has the minimum bounded scope
// identity to run.
func (c Config) Validate() error {
	_, err := c.validated()
	return err
}

func (c Config) validated() (Config, error) {
	collectorID := strings.TrimSpace(c.CollectorInstanceID)
	if collectorID == "" {
		return Config{}, fmt.Errorf("azure collector instance id is required")
	}
	if len(c.Targets) == 0 {
		return Config{}, fmt.Errorf("at least one azure scope target is required")
	}
	targets := make([]TargetConfig, 0, len(c.Targets))
	seen := make(map[string]struct{}, len(c.Targets))
	for i, target := range c.Targets {
		validated, err := target.validated()
		if err != nil {
			return Config{}, fmt.Errorf("target %d: %w", i, err)
		}
		key := validated.dedupeKey()
		if _, ok := seen[key]; ok {
			return Config{}, fmt.Errorf("target %d duplicates an earlier azure scope target", i)
		}
		seen[key] = struct{}{}
		targets = append(targets, validated)
	}
	return Config{CollectorInstanceID: collectorID, Targets: targets}, nil
}

func (t TargetConfig) validated() (TargetConfig, error) {
	tenantID := strings.TrimSpace(t.TenantID)
	if tenantID == "" {
		return TargetConfig{}, fmt.Errorf("tenant_id is required")
	}
	scopeKind := strings.TrimSpace(t.ScopeKind)
	if !validScopeKind(scopeKind) {
		return TargetConfig{}, fmt.Errorf("scope_kind %q must be one of subscription, management_group, tenant", t.ScopeKind)
	}
	providerScopeID := strings.TrimSpace(t.ProviderScopeID)
	if providerScopeID == "" {
		return TargetConfig{}, fmt.Errorf("provider_scope_id is required")
	}
	sourceLane := strings.TrimSpace(t.SourceLane)
	if sourceLane == "" {
		sourceLane = SourceLaneResourceGraph
	}
	if !validSourceLane(sourceLane) {
		return TargetConfig{}, fmt.Errorf("source_lane %q must be one of resource_graph, resource_changes, arm_fallback", t.SourceLane)
	}
	fencingToken := t.FencingToken
	if fencingToken == 0 {
		fencingToken = defaultFencingToken
	}
	if fencingToken < 0 {
		return TargetConfig{}, fmt.Errorf("fencing_token must not be negative")
	}
	return TargetConfig{
		TenantID:           tenantID,
		ScopeKind:          scopeKind,
		ProviderScopeID:    providerScopeID,
		ResourceTypeFamily: strings.ToLower(strings.TrimSpace(t.ResourceTypeFamily)),
		LocationBucket:     strings.ToLower(strings.TrimSpace(t.LocationBucket)),
		CredentialRef:      strings.TrimSpace(t.CredentialRef),
		SourceURI:          strings.TrimSpace(t.SourceURI),
		SourceLane:         sourceLane,
		FencingToken:       fencingToken,
	}, nil
}

// ScopeIDForTarget reports the durable scope ID for one target after
// validation and normalization, so host wiring and the local proof runner
// derive the exact scope the collector authorizes.
func ScopeIDForTarget(target TargetConfig) (string, error) {
	validated, err := target.validated()
	if err != nil {
		return "", err
	}
	return scopeIDForTarget(validated), nil
}

// dedupeKey identifies a target's bounded scope shard for duplicate detection.
// It excludes the credential reference and source URI because the shard
// identity is the scope, not the credential plumbing.
func (t TargetConfig) dedupeKey() string {
	return strings.Join([]string{
		t.TenantID,
		t.ScopeKind,
		t.ProviderScopeID,
		t.ResourceTypeFamily,
		t.LocationBucket,
		t.SourceLane,
	}, "|")
}

// scopeIDForTarget builds the durable scope ID for one Azure shard, following
// the contract layout
// azure:<tenant>:<scope_kind>:<provider_scope>:<resource_family>:<location>:<source_lane>.
// Empty narrowing buckets collapse to "all" so the scope stays stable and
// readable.
func scopeIDForTarget(target TargetConfig) string {
	return strings.Join([]string{
		CollectorKind,
		target.TenantID,
		target.ScopeKind,
		target.ProviderScopeID,
		orAll(target.ResourceTypeFamily),
		orAll(target.LocationBucket),
		target.SourceLane,
	}, ":")
}

func orAll(value string) string {
	if strings.TrimSpace(value) == "" {
		return "all"
	}
	return value
}

func validScopeKind(kind string) bool {
	switch kind {
	case ScopeKindSubscription, ScopeKindManagementGroup, ScopeKindTenant:
		return true
	default:
		return false
	}
}

func validSourceLane(lane string) bool {
	switch lane {
	case SourceLaneResourceGraph, SourceLaneResourceChanges, SourceLaneARMFallback:
		return true
	default:
		return false
	}
}
