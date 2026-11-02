// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
)

// manifestContract derives the SDK host contract from the shipped component
// manifest, so the binary validates its own output against the same
// declaration the conformance suite proves.
func manifestContract() sdk.Contract {
	return collector.ManifestContract()
}

// digestOf derives the opaque success digest for one completed collection: a
// hash over the ordered stable keys. It carries no identity and is safe to log
// and persist as the run's success marker.
func digestOf(result sdk.Result) string {
	var keys []string
	for _, fact := range result.Facts {
		keys = append(keys, fact.Kind+"|"+fact.StableKey)
	}
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}

type localOptions struct {
	pages            []string
	targetsJSON      string
	collectorID      string
	generationID     string
	redactionKeyFile string
	live             bool
}

// collectLocal runs one fixture-mode collection for a synthesized fixture
// claim and renders the SDK host result. It never calls Azure. The live
// opt-in reaches the gated seam and fails closed.
func collectLocal(opts localOptions, limits collector.ResourceUse) (resultWithMeta, error) {
	if opts.live {
		return resultWithMeta{}, collector.ErrLiveProviderGated
	}
	targets, err := localTargets(opts.targetsJSON)
	if err != nil {
		return resultWithMeta{}, err
	}
	// Local mode proves one scope per invocation under one synthesized
	// fixture claim. Multi-scope proof runs one invocation per scope (or the
	// host protocol, which sends one claim per scope); silently skipping
	// configured targets would corrupt the proof.
	if len(targets) != 1 {
		return resultWithMeta{}, fmt.Errorf("local fixture mode collects exactly one target per invocation, got %d", len(targets))
	}
	if len(opts.pages) == 0 {
		return resultWithMeta{}, fmt.Errorf("at least one fixture page is required")
	}
	key, err := loadKeyFile(opts.redactionKeyFile)
	if err != nil {
		return resultWithMeta{}, err
	}
	provider, err := collector.NewFixturePageProviderFromFiles(collector.ScopeAccess{}, opts.pages...)
	if err != nil {
		return resultWithMeta{}, err
	}
	config := collector.Config{CollectorInstanceID: opts.collectorID, Targets: targets}
	if err := config.Validate(); err != nil {
		return resultWithMeta{}, err
	}
	claim := fixtureClaim(opts.collectorID, targets, opts.generationID)
	return collectWithProvider(context.Background(), opts.collectorID, targets, claim, limits, provider, key)
}

// localTargets decodes the --targets JSON array, defaulting to the parity
// fixture target that matches the checked-in parity pages.
func localTargets(raw string) ([]collector.TargetConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return []collector.TargetConfig{{
			TenantID:           "tenant-abc",
			ScopeKind:          collector.ScopeKindSubscription,
			ProviderScopeID:    "11111111-1111-1111-1111-111111111111",
			ResourceTypeFamily: "microsoft.compute",
			LocationBucket:     "eastus",
			CredentialRef:      "azure-read-only-spn",
			SourceURI:          collector.ResourceGraphEndpoint,
		}}, nil
	}
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode --targets: %w", err)
	}
	items := make([]any, 0, len(decoded))
	for _, item := range decoded {
		items = append(items, item)
	}
	return parseTargets(items)
}

// fixtureClaim synthesizes the fixture-only claim for a local run. The scope
// is the first configured target's scope; the generation defaults to a
// per-sweep derivation unless --generation-id pins it for proof runs.
func fixtureClaim(collectorID string, targets []collector.TargetConfig, generationID string) sdk.Claim {
	scopeID, scopeKind := "", ""
	if len(targets) > 0 {
		var err error
		scopeID, err = collector.ScopeIDForTarget(targets[0])
		if err != nil {
			scopeID = ""
		}
		scopeKind = strings.TrimSpace(targets[0].ScopeKind)
	}
	if strings.TrimSpace(generationID) == "" {
		generationID = "fixture-" + time.Now().UTC().Format("20060102-150405")
	}
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID:   collector.ComponentID,
		InstanceID:    collectorID,
		CollectorKind: collector.CollectorKind,
		SourceSystem:  collector.SourceSystem,
		Scope:         sdk.Scope{ID: scopeID, Kind: scopeKind},
		SourceRunID:   "fixture-run",
		GenerationID:  generationID,
		WorkItemID:    "fixture-work-item",
		FencingToken:  "1",
		Attempt:       1,
		Deadline:      now.Add(time.Hour),
		ConfigHandle:  "fixture-config",
	}
}
