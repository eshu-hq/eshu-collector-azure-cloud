// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

// Package collector is the standalone Eshu Azure cloud collector satellite.
//
// It observes Azure provider truth through a read-only page-provider seam and
// emits versioned source facts through the published Eshu SDK stdio contract
// (collector-sdk/v1alpha1). Fixture mode is the default: page providers serve
// checked-in Resource Graph fixtures and no live Azure call is possible. Live
// transport stays behind an explicit opt-in and a gated factory that fails
// closed until a read-only adapter is injected.
//
// Core keeps admission, identity, projection, persistence, and recovery. This
// module vendors no core-owned package: it imports only the published SDK
// contracts (collector, factschema), the standard library, and yaml for its
// own manifest document.
package collector
