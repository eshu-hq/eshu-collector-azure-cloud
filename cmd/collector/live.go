// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/marker"
)

// newLiveProviderFactory builds the read-only live Resource Graph provider
// factory for the live stdio runner. It is a package variable so tests can
// inject a hermetic fake factory without live Azure access. The credential is
// resolved from the ambient workload identity (managed identity, workload
// identity federation, or environment), never from the configuration document;
// the credentialRef is a name only and gates the factory so the runner never
// falls back to an unnamed credential silently.
var newLiveProviderFactory = defaultLiveProviderFactory

// defaultLiveProviderFactory resolves the ambient Azure credential and wraps
// the official Resource Graph SDK client behind the read-only
// LiveProviderFactory. It performs no live Azure call itself; the runner reads
// only after the host grants a claim the satellite authorizes.
func defaultLiveProviderFactory(
	_ context.Context,
	credentialRef string,
) (collector.PageProviderFactory, error) {
	if strings.TrimSpace(credentialRef) == "" {
		return nil, errors.New("azure live credential_ref is required")
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("azure default credential unavailable: %w", err)
	}
	client, err := collector.NewAzureSDKResourceGraphClientFromCredential(credential, nil)
	if err != nil {
		return nil, err
	}
	return collector.LiveProviderFactory{ResourceGraphClient: client}, nil
}

// singleLiveCredentialRef requires one shared credential name across the live
// targets: one collector instance resolves one ambient workload identity,
// never a per-target secret value.
func singleLiveCredentialRef(targets []collector.TargetConfig) (string, error) {
	if len(targets) == 0 {
		return "", fmt.Errorf("live collection requires at least one target")
	}
	credentialRef := ""
	for i, target := range targets {
		ref := strings.TrimSpace(target.CredentialRef)
		if ref == "" {
			return "", fmt.Errorf("live target %d requires a credential reference name", i)
		}
		if credentialRef == "" {
			credentialRef = ref
		} else if credentialRef != ref {
			return "", fmt.Errorf("live collection requires one credential reference per collector instance")
		}
	}
	return credentialRef, nil
}

// collectLivePlan runs one host-claimed live collection: the explicit opt-in
// plus an authorized claim reach the injected read-only adapter. Claim
// authorization happens inside Source.Collect before any provider call, so an
// unauthorized or stale claim fails closed without touching the transport.
func collectLivePlan(ctx context.Context, plan stdioPlan, key marker.Key) (resultWithMeta, error) {
	config := collector.Config{CollectorInstanceID: plan.collectorID, Targets: plan.targets}
	// The host claim authorizes before any adapter exists: an unauthorized or
	// stale claim fails closed with no credential resolved and no transport
	// constructed.
	authorized := &collector.Source{Config: config, RedactionKey: key, Clock: time.Now}
	if _, err := authorized.AuthorizeClaim(plan.claim); err != nil {
		return resultWithMeta{}, err
	}
	credentialRef, err := singleLiveCredentialRef(plan.targets)
	if err != nil {
		return resultWithMeta{}, err
	}
	factory, err := newLiveProviderFactory(ctx, credentialRef)
	if err != nil {
		return resultWithMeta{}, err
	}
	source := &collector.Source{
		Config:          config,
		ProviderFactory: factory,
		RedactionKey:    key,
		Clock:           time.Now,
	}
	result, err := source.Collect(ctx, plan.claim)
	if err != nil {
		return resultWithMeta{}, err
	}
	if err := enforceLimits(result, plan.limits); err != nil {
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
