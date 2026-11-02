// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// stableID returns a deterministic identifier for one fact-adjacent payload.
// It hashes the fact type together with the normalized identity map, so the
// same identity yields the same id on every collector run, host, and replay.
//
// The algorithm is the shared Eshu stable-id encoding (SHA-256 over
// {"fact_type":..., "identity": normalize(...)} with UTC RFC3339Nano time
// normalization and recursive map/slice normalization), ported here so the
// satellite derives byte-identical stable keys and fact ids without importing
// a core-owned package.
func stableID(factType string, identity map[string]any) string {
	record := map[string]any{
		"fact_type": factType,
		"identity":  normalizeStableValue(identity),
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("marshal stable id payload: %v", err))
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func normalizeStableValue(value any) any {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cloned[key] = normalizeStableValue(nested)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = normalizeStableValue(typed[i])
		}
		return cloned
	case []string:
		cloned := make([]any, len(typed))
		for i := range typed {
			cloned[i] = typed[i]
		}
		return cloned
	default:
		return typed
	}
}
