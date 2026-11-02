// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/collector/conformance"
)

// GrantRequest is the collector-side shape of a first-party producer grant
// request. The core issues the signed grant; the collector stores only this
// metadata and never mints canonical identity itself. Revoked grants never
// match and a zero expiry is already expired, mirroring the host.
type GrantRequest struct {
	ProducerID     string    `json:"producer_id"`
	Version        string    `json:"version"`
	Kind           string    `json:"kind"`
	SchemaVersions []string  `json:"schema_versions"`
	Scope          string    `json:"scope"`
	ExpiresAt      time.Time `json:"expires_at"`
	Revoked        bool      `json:"revoked"`
}

// TestProducerGrantContract proves the collector's grant wiring follows the
// first-party producer grant contract: a core-owned kind needs a live grant
// naming this producer/version/kind/schema with a scope declared in
// spec.collectorKinds, and anything outside the grant fails closed. Which kinds
// are core-owned is the host's decision, not the collector's: see
// uncoveredEmissions.
func TestProducerGrantContract(t *testing.T) {
	t.Parallel()

	now := time.Now()
	scopes := loadManifest(t).Spec.CollectorKinds
	grant := GrantRequest{
		ProducerID:     ComponentID,
		Version:        ComponentVersion,
		Kind:           "azure_cloud_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          CollectorKind,
		ExpiresAt:      now.Add(time.Hour),
	}
	covers := func(g GrantRequest, producer, kind, schema string) bool {
		return grantCovers([]GrantRequest{g}, producer, ComponentVersion, kind, schema, scopes, now)
	}
	if !covers(grant, ComponentID, "azure_cloud_resource", "1.0.0") {
		t.Fatal("live grant must authorize its kind/schema/scope")
	}
	// A scope outside the manifest's collector kinds never matches.
	outOfScope := grant
	outOfScope.Scope = "other"
	if covers(outOfScope, ComponentID, "azure_cloud_resource", "1.0.0") {
		t.Fatal("out-of-scope grant must fail closed")
	}
	if covers(grant, ComponentID, "azure_cloud_resource", "2.0.0") {
		t.Fatal("ungranted schema version must fail closed")
	}
	if covers(grant, ComponentID, "azure_dns_record", "1.0.0") {
		t.Fatal("ungranted kind must fail closed")
	}
	if covers(grant, "dev.eshu.other.collector", "azure_cloud_resource", "1.0.0") {
		t.Fatal("foreign producer must fail closed")
	}
	revoked := grant
	revoked.Revoked = true
	if covers(revoked, ComponentID, "azure_cloud_resource", "1.0.0") {
		t.Fatal("revoked grant must fail closed")
	}
	expired := grant
	expired.ExpiresAt = now.Add(-time.Minute)
	if covers(expired, ComponentID, "azure_cloud_resource", "1.0.0") {
		t.Fatal("expired grant must fail closed")
	}
	zeroExpiry := grant
	zeroExpiry.ExpiresAt = time.Time{}
	if covers(zeroExpiry, ComponentID, "azure_cloud_resource", "1.0.0") {
		t.Fatal("zero expiry is already expired and must fail closed")
	}
	// The live example grants authorize every declared kind, while a revoked
	// grant alone authorizes nothing: revocation constrains granted kinds
	// instead of silently passing them.
	if problems := uncoveredEmissions(loadManifest(t), loadGrantExamples(t), now); len(problems) != 0 {
		t.Fatalf("live example grants left emissions uncovered: %v", problems)
	}
	if problems := uncoveredEmissions(loadManifest(t), []GrantRequest{revoked}, now); len(problems) == 0 {
		t.Fatal("revoked grant authorized emissions, want fail-closed")
	}
	// Conflicting producers never share one scope+generation: the collector
	// emits under its own claim scope only.
	result := mustCollect(t, testClaim(), "testdata/resources_page1.json", "testdata/resources_page2.json")
	for _, fact := range result.Facts {
		if fact.SourceRef.ScopeID != testClaim().Scope.ID {
			t.Fatalf("fact scope %q escapes the claim scope", fact.SourceRef.ScopeID)
		}
	}
}

// grantCovers reports whether any live grant authorizes the
// (producer, version, kind, schema) tuple in a manifest collector-kind scope.
func grantCovers(grants []GrantRequest, producer, version, kind, schema string, scopes []string, now time.Time) bool {
	for _, grant := range grants {
		if grant.Revoked {
			continue
		}
		if grant.ExpiresAt.IsZero() || !grant.ExpiresAt.After(now) {
			continue
		}
		if grant.ProducerID != producer || grant.Version != version && !grantVersionMatches(grant.Version, version) {
			continue
		}
		if grant.Kind != kind {
			continue
		}
		if !slices.Contains(grant.SchemaVersions, schema) {
			continue
		}
		if !slices.Contains(scopes, grant.Scope) {
			continue
		}
		return true
	}
	return false
}

// grantVersionMatches tolerates a "v"-prefixed grant version naming the same
// release, so a v0.1.0 issuance covers a 0.1.0 declaration.
func grantVersionMatches(have, want string) bool {
	return strings.TrimPrefix(have, "v") == strings.TrimPrefix(want, "v")
}

// uncoveredEmissions lists every manifest-declared emission no live grant
// covers. A kind in this collector's own namespace is exempt only when it sits
// in the collector's own namespace; every other kind needs exact grant
// coverage.
func uncoveredEmissions(manifest conformance.Manifest, grants []GrantRequest, now time.Time) []string {
	var problems []string
	for _, family := range manifest.Spec.EmittedFacts {
		covered := false
		for _, schema := range family.SchemaVersions {
			if grantCovers(grants, manifest.Metadata.ID, manifest.Metadata.Version, family.Kind, schema, manifest.Spec.CollectorKinds, now) {
				covered = true
				break
			}
		}
		if !covered {
			problems = append(problems, family.Kind)
		}
	}
	return problems
}

// loadGrantExamples decodes the checked-in issuance request shape: one grant
// object per core-owned kind the manifest declares (the CLI binds one kind per
// grant), all naming this producer, version, and collector-kind scope.
func loadGrantExamples(t *testing.T) []GrantRequest {
	t.Helper()

	raw, err := os.ReadFile("grant.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var grants []GrantRequest
	if err := json.Unmarshal(raw, &grants); err != nil {
		t.Fatal(err)
	}
	if len(grants) == 0 {
		t.Fatal("grant.example.json declares no grants")
	}
	return grants
}

// TestGrantExampleParses proves every checked-in grant example matches the
// request shape operators submit for issuance.
func TestGrantExampleParses(t *testing.T) {
	t.Parallel()

	for _, grant := range loadGrantExamples(t) {
		if strings.TrimSpace(grant.ProducerID) == "" || strings.TrimSpace(grant.Scope) == "" {
			t.Fatalf("grant example missing producer or scope: %+v", grant)
		}
		if strings.TrimSpace(grant.Kind) == "" || len(grant.SchemaVersions) == 0 {
			t.Fatalf("grant example missing kind or schema versions: %+v", grant)
		}
	}
}

// TestGrantExampleScopeIsCollectorKind proves the example grant is scoped the
// way the host matches it. The host compares a grant's scope against the
// manifest's spec.collectorKinds, not against a claim scope ID such as the
// activation config's host.scope.id, so an example scoped by ID would be
// issued and then never match at install or activation.
func TestGrantExampleScopeIsCollectorKind(t *testing.T) {
	t.Parallel()

	manifest := loadManifest(t)
	for _, grant := range loadGrantExamples(t) {
		if !slices.Contains(manifest.Spec.CollectorKinds, grant.Scope) {
			t.Fatalf("grant scope %q is not a collector kind in the manifest %v", grant.Scope, manifest.Spec.CollectorKinds)
		}
		if grant.Scope != CollectorKind {
			t.Fatalf("grant scope %q, want the collector kind %q", grant.Scope, CollectorKind)
		}
		if grant.ProducerID != ComponentID {
			t.Fatalf("grant producer %q, want this collector %q", grant.ProducerID, ComponentID)
		}
		if grant.Version != manifest.Metadata.Version {
			t.Fatalf("grant version %q, want manifest version %q", grant.Version, manifest.Metadata.Version)
		}
	}
}

// TestManifestEmissionsAreAuthorized proves every kind the shipped manifest
// declares is covered by a grant in grant.example.json. The check derives its
// verdict from the manifest alone: the collector cannot import the core
// registry, so it never guesses which kinds are core-owned.
func TestManifestEmissionsAreAuthorized(t *testing.T) {
	t.Parallel()

	if problems := uncoveredEmissions(loadManifest(t), loadGrantExamples(t), time.Now()); len(problems) != 0 {
		t.Fatalf("shipped manifest has unauthorized emissions: %v", problems)
	}
}

// TestUncoveredEmissionsRejectsUnauthorizedKind proves a kind outside the
// grant set never passes, including a dotted core-looking kind.
func TestUncoveredEmissionsRejectsUnauthorizedKind(t *testing.T) {
	t.Parallel()

	now := time.Now()
	manifest := loadManifest(t)
	// Narrow to a single dotted core-looking kind so the verdict isolates the
	// grant rule instead of the eight shipped families.
	manifest.Spec.EmittedFacts = []conformance.FactFamily{{
		Kind:             "incident.record",
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []string{"reported"},
	}}
	grant := GrantRequest{
		ProducerID:     ComponentID,
		Version:        manifest.Metadata.Version,
		Kind:           "incident.record",
		SchemaVersions: []string{"1.0.0"},
		Scope:          CollectorKind,
		ExpiresAt:      now.Add(time.Hour),
	}
	if problems := uncoveredEmissions(manifest, nil, now); len(problems) != 1 || problems[0] != "incident.record" {
		t.Fatalf("ungranted kind verdict = %v, want [incident.record]", problems)
	}
	revoked := grant
	revoked.Revoked = true
	if problems := uncoveredEmissions(manifest, []GrantRequest{revoked}, now); len(problems) == 0 {
		t.Fatal("revoked grant passed, want fail-closed")
	}
	if problems := uncoveredEmissions(manifest, []GrantRequest{grant}, now); len(problems) != 0 {
		t.Fatalf("exact grant left emissions uncovered: %v", problems)
	}
}
