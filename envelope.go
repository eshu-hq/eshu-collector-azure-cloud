// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
	"github.com/eshu-hq/eshu-sdk/factschema"
	azurev1 "github.com/eshu-hq/eshu-sdk/factschema/azure/v1"
)

// Azure fact schema versions. Every Azure fact kind ships its first schema;
// the version rides each emitted fact so the reducer decodes a known shape.
const (
	// AzureCloudResourceSchemaVersion is the first Azure cloud resource schema.
	AzureCloudResourceSchemaVersion = "1.0.0"
	// AzureCloudRelationshipSchemaVersion is the first Azure relationship schema.
	AzureCloudRelationshipSchemaVersion = "1.0.0"
	// AzureTagObservationSchemaVersion is the first Azure tag observation schema.
	AzureTagObservationSchemaVersion = "1.0.0"
	// AzureIdentityObservationSchemaVersion is the first Azure identity schema.
	AzureIdentityObservationSchemaVersion = "1.0.0"
	// AzureResourceChangeSchemaVersion is the first Azure resource change schema.
	AzureResourceChangeSchemaVersion = "1.0.0"
	// AzureDNSRecordSchemaVersion is the first Azure DNS record schema.
	AzureDNSRecordSchemaVersion = "1.0.0"
	// AzureImageReferenceSchemaVersion is the first Azure image reference schema.
	AzureImageReferenceSchemaVersion = "1.0.0"
	// AzureCollectionWarningSchemaVersion is the first Azure collection warning
	// schema.
	AzureCollectionWarningSchemaVersion = "1.0.0"
)

// AzureExtensionSchemaVersion versions the provider-specific extension object
// embedded in azure_cloud_resource facts. It is independent of the fact schema
// version so the extension shape can evolve without a fact-kind migration.
const AzureExtensionSchemaVersion = "1.0.0"

// NewResourceEnvelope builds the durable azure_cloud_resource fact for one
// normalized Resource Graph resource. It preserves the raw ARM resource ID,
// adds normalized identity fields, embeds a versioned and redacted
// provider-specific extension object, and records the redaction policy version.
//
// The stable fact key is derived from the normalized ARM identity, resource
// type, and source lane only, so churn in volatile extension or tag values does
// not split idempotent re-emission of the same generation.
func NewResourceEnvelope(observation ResourceObservation) (sdk.Fact, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return sdk.Fact{}, err
	}
	armResourceID := strings.TrimSpace(observation.ARMResourceID)
	if armResourceID == "" {
		return sdk.Fact{}, fmt.Errorf("azure resource observation requires arm_resource_id")
	}
	identity := observation.Identity
	if strings.TrimSpace(identity.Normalized) == "" {
		parsed, err := ParseARMIdentity(armResourceID)
		if err != nil {
			return sdk.Fact{}, fmt.Errorf("normalize arm identity: %w", err)
		}
		identity = parsed
	}

	stableKey := stableID(factschema.FactKindAzureCloudResource, map[string]any{
		"normalized_id": identity.Normalized,
		"resource_type": identity.ResourceType,
		"source_lane":   observation.Boundary.SourceLane,
		"tenant_id":     observation.Boundary.TenantID,
	})

	redaction := RedactExtension(observation.RawExtension)
	extension := map[string]any{
		"schema_version": AzureExtensionSchemaVersion,
		"data":           redaction.Extension,
		"redacted":       redaction.Redacted,
		"redacted_keys":  redaction.RedactedKeys,
		"managed_by":     strings.TrimSpace(observation.ManagedBy),
		"api_version":    strings.TrimSpace(observation.APIVersion),
		"has_identity":   observation.HasIdentity,
	}

	attributes := map[string]any{
		"collector_kind":           CollectorKind,
		"collector_instance_id":    observation.Boundary.CollectorInstanceID,
		"tenant_id":                observation.Boundary.TenantID,
		"scope_kind":               observation.Boundary.ScopeKind,
		"provider_scope_id":        observation.Boundary.ProviderScopeID,
		"source_lane":              observation.Boundary.SourceLane,
		"arm_resource_id":          armResourceID,
		"subscription_id":          identity.SubscriptionID,
		"resource_group":           identity.ResourceGroup,
		"provider_namespace":       identity.ProviderNamespace,
		"resource_type":            identity.ResourceType,
		"resource_name":            identity.ResourceName,
		"normalized_resource_id":   identity.Normalized,
		"location":                 strings.TrimSpace(observation.Boundary.LocationBucket),
		"kind":                     strings.TrimSpace(observation.Kind),
		"sku_class":                strings.TrimSpace(observation.SKUClass),
		"tags":                     cloneStringMap(observation.Tags),
		"provider_time":            timeOrNil(observation.ProviderTime),
		"redaction_policy_version": RedactionPolicyVersion,
		"extension":                extension,
	}
	resourceName := identity.ResourceName
	providerNamespace := identity.ProviderNamespace
	normalizedID := identity.Normalized
	payload, err := factschema.EncodeAzureCloudResource(azurev1.CloudResource{
		ARMResourceID:        armResourceID,
		ResourceType:         identity.ResourceType,
		SubscriptionID:       identity.SubscriptionID,
		Location:             strings.TrimSpace(observation.Boundary.LocationBucket),
		NormalizedResourceID: &normalizedID,
		ResourceName:         &resourceName,
		ProviderNamespace:    &providerNamespace,
		Attributes:           attributes,
	})
	if err != nil {
		return sdk.Fact{}, fmt.Errorf("encode azure_cloud_resource payload: %w", err)
	}

	return newFact(
		observation.Boundary,
		factschema.FactKindAzureCloudResource,
		AzureCloudResourceSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized),
		observation.SourceURI,
		payload,
	), nil
}

// NewTagObservationEnvelope builds the durable azure_tag_observation fact for
// one resource's tags. Tag values are fingerprinted with the provided redaction
// key so tag value text never reaches durable facts; tag keys are preserved as
// correlation taxonomy. The stable fact key is derived from the normalized ARM
// identity, resource type, and source lane only, so tag value churn does not
// split idempotent re-emission of the same generation.
//
// It returns an error when the observation carries no usable tags (an empty tag
// observation is missing evidence, not a clean match) or when the redaction key
// is zero (fingerprinting must never run keyless). Callers must skip emission
// for untagged resources rather than treating the error as fatal.
func NewTagObservationEnvelope(observation ResourceObservation, key marker.Key) (sdk.Fact, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return sdk.Fact{}, err
	}
	if key.IsZero() {
		return sdk.Fact{}, fmt.Errorf("azure tag observation requires a redaction key")
	}
	armResourceID := strings.TrimSpace(observation.ARMResourceID)
	if armResourceID == "" {
		return sdk.Fact{}, fmt.Errorf("azure tag observation requires arm_resource_id")
	}
	identity := observation.Identity
	if strings.TrimSpace(identity.Normalized) == "" {
		parsed, err := ParseARMIdentity(armResourceID)
		if err != nil {
			return sdk.Fact{}, fmt.Errorf("normalize arm identity: %w", err)
		}
		identity = parsed
	}

	fingerprints, tagKeys, truncated := FingerprintTagValues(observation.Tags, key)
	if len(fingerprints) == 0 {
		return sdk.Fact{}, fmt.Errorf("azure tag observation requires at least one tag")
	}

	stableKey := stableID(factschema.FactKindAzureTagObservation, map[string]any{
		"normalized_id": identity.Normalized,
		"resource_type": identity.ResourceType,
		"source_lane":   observation.Boundary.SourceLane,
		"tenant_id":     observation.Boundary.TenantID,
	})

	subscriptionID := identity.SubscriptionID
	resourceGroup := identity.ResourceGroup
	providerNamespace := identity.ProviderNamespace
	resourceName := identity.ResourceName
	tagCount := len(fingerprints)
	payload, err := factschema.EncodeAzureTagObservation(azurev1.TagObservation{
		ARMResourceID:          armResourceID,
		NormalizedResourceID:   identity.Normalized,
		ResourceType:           identity.ResourceType,
		TagValueFingerprints:   fingerprints,
		SubscriptionID:         &subscriptionID,
		ResourceGroup:          &resourceGroup,
		ProviderNamespace:      &providerNamespace,
		ResourceName:           &resourceName,
		TagKeys:                tagKeys,
		TagCount:               &tagCount,
		TagTruncated:           &truncated,
		ProviderTime:           timeStringPtr(observation.ProviderTime),
		RedactionPolicyVersion: stringPtr(RedactionPolicyVersion),
	})
	if err != nil {
		return sdk.Fact{}, fmt.Errorf("encode azure_tag_observation payload: %w", err)
	}
	addAzureBoundaryPayload(payload, observation.Boundary)
	payload["tenant_id"] = observation.Boundary.TenantID
	payload["scope_kind"] = observation.Boundary.ScopeKind
	payload["provider_scope_id"] = observation.Boundary.ProviderScopeID
	payload["source_lane"] = observation.Boundary.SourceLane

	return newFact(
		observation.Boundary,
		factschema.FactKindAzureTagObservation,
		AzureTagObservationSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized),
		observation.SourceURI,
		payload,
	), nil
}

// NewWarningEnvelope builds the durable azure_collection_warning fact for one
// explicit partial, permission-hidden, truncation, throttle, fallback,
// stale, unsupported, or redaction outcome. Warning facts are how the collector
// reports incomplete coverage as evidence instead of silent success.
func NewWarningEnvelope(observation WarningObservation) (sdk.Fact, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return sdk.Fact{}, err
	}
	warningKind := strings.TrimSpace(observation.WarningKind)
	if warningKind == "" {
		return sdk.Fact{}, fmt.Errorf("azure warning observation requires warning_kind")
	}
	outcome := strings.TrimSpace(observation.Outcome)
	if outcome == "" {
		outcome = OutcomePartial
	}

	stableKey := stableID(factschema.FactKindAzureCollectionWarning, map[string]any{
		"generation_id":   observation.Boundary.GenerationID,
		"resource_family": observation.Boundary.ResourceTypeFamily,
		"scope_id":        observation.Boundary.ScopeID,
		"source_lane":     observation.Boundary.SourceLane,
		"warning_kind":    warningKind,
	})

	hiddenCount, err := nonNegativeInt32PayloadCount("azure_collection_warning hidden_resource_count", observation.HiddenResourceCount)
	if err != nil {
		return sdk.Fact{}, err
	}
	payload, err := factschema.EncodeAzureCollectionWarning(azurev1.CollectionWarning{
		WarningKind:         warningKind,
		Outcome:             outcome,
		ResourceFamily:      stringPtr(observation.Boundary.ResourceTypeFamily),
		Retryable:           &observation.Retryable,
		HiddenResourceCount: &hiddenCount,
		Message:             stringPtr(sanitizeWarningMessage(observation.Message)),
	})
	if err != nil {
		return sdk.Fact{}, fmt.Errorf("encode azure_collection_warning payload: %w", err)
	}
	addAzureBoundaryPayload(payload, observation.Boundary)
	payload["tenant_id"] = observation.Boundary.TenantID
	payload["scope_kind"] = observation.Boundary.ScopeKind
	payload["provider_scope_id"] = observation.Boundary.ProviderScopeID
	payload["resource_family"] = observation.Boundary.ResourceTypeFamily
	payload["source_lane"] = observation.Boundary.SourceLane
	payload["hidden_resource_count"] = observation.HiddenResourceCount

	return newFact(
		observation.Boundary,
		factschema.FactKindAzureCollectionWarning,
		AzureCollectionWarningSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, warningKind),
		observation.SourceURI,
		payload,
	), nil
}

func newFact(
	boundary Boundary,
	factKind string,
	schemaVersion string,
	stableKey string,
	sourceRecordID string,
	sourceURI string,
	payload map[string]any,
) sdk.Fact {
	observedAt := boundary.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	sourceURI = strings.TrimSpace(sourceURI)
	if sourceURI == "" {
		sourceURI = ResourceGraphEndpoint
	}
	return sdk.Fact{
		Kind:             factKind,
		SchemaVersion:    schemaVersion,
		StableKey:        stableKey,
		SourceConfidence: sdk.SourceConfidenceReported,
		ObservedAt:       observedAt,
		SourceRef: sdk.SourceRef{
			SourceSystem: SourceSystem,
			ScopeID:      boundary.ScopeID,
			GenerationID: boundary.GenerationID,
			FactKey:      stableKey,
			URI:          sourceURI,
			RecordID:     sourceRecordID,
		},
		Payload: payload,
	}
}

func validateBoundary(boundary Boundary) error {
	switch {
	case strings.TrimSpace(boundary.CollectorInstanceID) == "":
		return fmt.Errorf("azure observation requires collector_instance_id")
	case strings.TrimSpace(boundary.TenantID) == "":
		return fmt.Errorf("azure observation requires tenant_id")
	case !validScopeKind(boundary.ScopeKind):
		return fmt.Errorf("azure observation requires a valid scope_kind, got %q", boundary.ScopeKind)
	case !validSourceLane(boundary.SourceLane):
		return fmt.Errorf("azure observation requires a valid source_lane, got %q", boundary.SourceLane)
	case strings.TrimSpace(boundary.ScopeID) == "":
		return fmt.Errorf("azure observation requires scope_id")
	case strings.TrimSpace(boundary.GenerationID) == "":
		return fmt.Errorf("azure observation requires generation_id")
	case boundary.FencingToken <= 0:
		return fmt.Errorf("azure observation fencing_token must be positive")
	default:
		return nil
	}
}

func sourceRecordID(candidate, fallback string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate != "" {
		return candidate
	}
	return strings.TrimSpace(fallback)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func addAzureBoundaryPayload(payload map[string]any, boundary Boundary) {
	payload["collector_kind"] = CollectorKind
	payload["collector_instance_id"] = boundary.CollectorInstanceID
}

func stringPtr(value string) *string {
	return &value
}

func timeStringPtr(input *time.Time) *string {
	if input == nil || input.IsZero() {
		return nil
	}
	value := input.UTC().Format(time.RFC3339Nano)
	return &value
}

func timeOrNil(input *time.Time) any {
	if input == nil || input.IsZero() {
		return nil
	}
	return input.UTC()
}

func sanitizeWarningMessage(message string) string {
	const maxWarningMessageLength = 240
	sanitized := strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if len(sanitized) <= maxWarningMessageLength {
		return sanitized
	}
	return sanitized[:maxWarningMessageLength-3] + "..."
}
