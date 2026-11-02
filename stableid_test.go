// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"testing"
)

// TestStableIDMatchesInTreeGoldens proves the satellite's stable-id encoding
// is byte-identical to the shared Eshu derivation: the same (factType,
// identity) inputs as the in-tree oracle produce the same ids. Stable keys are
// the convergence contract for idempotent re-emission, so this parity is
// load-bearing, not cosmetic.
func TestStableIDMatchesInTreeGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		factType string
		identity map[string]any
		want     string
	}{
		{
			name:     "documentation section identity",
			factType: "documentation_section",
			identity: map[string]any{
				"document_id": "doc:confluence:12345",
				"revision_id": "17",
				"section_id":  "section:deployment",
			},
			want: "c02b58aad94108e01009dae9e17f789aa644aafa58d28780cbc803aa3d3da6fc",
		},
		{
			name:     "aws resource identity",
			factType: "aws_resource",
			identity: map[string]any{
				"account_id": "111122223333",
				"arn":        "arn:aws:s3:::example-bucket",
				"region":     "us-east-1",
			},
			want: "0d30a784e7a8eeefa058bc98a048eb56659409ad2cf2bc18284c2da969548301",
		},
		{
			name:     "empty identity still yields an id",
			factType: "empty_identity",
			identity: map[string]any{},
			want:     "0d9d6eec7c0474da6346f8c0827ac4813a7846b2d667af98676a1dd156f8db62",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := stableID(test.factType, test.identity); got != test.want {
				t.Fatalf("stableID(%q, ...) = %q, want in-tree golden %q", test.factType, got, test.want)
			}
		})
	}
}
