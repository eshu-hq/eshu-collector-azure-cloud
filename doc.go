// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

// Package collector is the standalone Eshu Azure cloud collector satellite.
//
// It observes Azure provider truth through a read-only page-provider seam and
// emits versioned source facts through the published Eshu SDK stdio contract
// (collector-sdk/v1alpha1). Fixture mode is the default: page providers serve
// checked-in Resource Graph fixtures and issue no live Azure calls. Live
// transport ships behind the explicit --live / "live": true opt-in plus a
// host claim the satellite authorizes before any adapter is built; the
// zero-value factory still fails closed, and tenant-backed proof stays
// deferred until a tenant exists.
//
// Core keeps admission, identity, projection, persistence, and recovery. This
// module vendors no core-owned package: besides the published SDK contracts
// (collector, factschema), the standard library, and yaml for its own
// manifest document, it imports only the official Azure SDK modules the live
// transport needs (azcore, azidentity, armresourcegraph, armresources),
// pinned in go.mod.
package collector
