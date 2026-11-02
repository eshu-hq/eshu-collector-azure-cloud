// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

func TestParseARMIdentityAcceptsNestedResource(t *testing.T) {
	t.Parallel()

	id := "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg-1/providers/Microsoft.Network/virtualNetworks/vnet-1/subnets/subnet-1"
	identity, err := ParseARMIdentity(id)
	if err != nil {
		t.Fatalf("ParseARMIdentity error = %v", err)
	}
	if identity.SubscriptionID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("SubscriptionID = %q", identity.SubscriptionID)
	}
	if identity.ResourceGroup != "rg-1" {
		t.Errorf("ResourceGroup = %q", identity.ResourceGroup)
	}
	if identity.ProviderNamespace != "microsoft.network" {
		t.Errorf("ProviderNamespace = %q", identity.ProviderNamespace)
	}
	if identity.ResourceType != "microsoft.network/virtualnetworks/subnets" {
		t.Errorf("ResourceType = %q", identity.ResourceType)
	}
	if identity.ResourceName != "subnet-1" {
		t.Errorf("ResourceName = %q", identity.ResourceName)
	}
	if identity.Normalized != strings.ToLower(id) {
		t.Errorf("Normalized = %q, want lowercased input", identity.Normalized)
	}
}

func TestParseARMIdentityRejectsMalformedIDs(t *testing.T) {
	t.Parallel()

	for _, id := range []string{
		"",
		"   ",
		"subscriptions/abc/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
		"/subscriptions/abc/providers/Microsoft.Compute/virtualMachines",
		"/tenants/abc/providers/Microsoft.Compute/virtualMachines/vm",
		"/subscriptions/abc/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm/extra",
		"/subscriptions//resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
	} {
		if _, err := ParseARMIdentity(id); err == nil {
			t.Errorf("ParseARMIdentity(%q) succeeded, want error", id)
		}
	}
}

func TestParseARMIdentityIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	upper := "/SUBSCRIPTIONS/AAA/RESOURCEGROUPS/RG/PROVIDERS/MICROSOFT.COMPUTE/VIRTUALMACHINES/VM"
	lower := "/subscriptions/aaa/resourcegroups/rg/providers/microsoft.compute/virtualmachines/vm"
	upperIdentity, err := ParseARMIdentity(upper)
	if err != nil {
		t.Fatalf("ParseARMIdentity(upper) error = %v", err)
	}
	lowerIdentity, err := ParseARMIdentity(lower)
	if err != nil {
		t.Fatalf("ParseARMIdentity(lower) error = %v", err)
	}
	if upperIdentity.Normalized != lowerIdentity.Normalized {
		t.Errorf("case variants normalized differently: %q vs %q", upperIdentity.Normalized, lowerIdentity.Normalized)
	}
}
