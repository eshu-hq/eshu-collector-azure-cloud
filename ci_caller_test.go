// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

// Contract test for the thin reusable caller in .github/workflows/ci.yml
// (issue #4): the lane bodies live in the pinned eshu-hq/.github reusable,
// so this file pins the caller-side wiring that must not drift — the exact
// reusable SHA, the required ci-complete context, the zero floor #2 owns,
// and the filter-only image gate #3 owns.
package collector

import (
	"os"
	"strings"
	"testing"
)

const pinnedReusable = "uses: eshu-hq/.github/.github/workflows/go-collector-ci.yml@4d01ab90baf5dd77c21cc1ee6bdf701332613449 # v2"

func readCIYAML(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("ci.yml is empty")
	}
	return string(raw)
}

// TestCallerPinsReusableSHA keeps a moved tag from changing what CI
// executes: the lanes job must name the exact v2 commit, never the tag.
func TestCallerPinsReusableSHA(t *testing.T) {
	t.Parallel()
	if text := readCIYAML(t); !strings.Contains(text, pinnedReusable) {
		t.Errorf("ci.yml lacks the pinned reusable %q", pinnedReusable)
	}
}

// TestCallerKeepsRequiredContext keeps branch protection churn-free: the
// ci-complete aggregator stays a caller job needing the classifier and the
// lanes, so the required context name never changes.
func TestCallerKeepsRequiredContext(t *testing.T) {
	t.Parallel()
	text := readCIYAML(t)
	// `if: always()` is load-bearing: without it a failed lanes job skips
	// ci-complete instead of failing it, and the required context goes
	// silent. Pin it alongside the name and the needs.
	for _, want := range []string{"ci-complete:", "needs: [changes, lanes]", "if: always()"} {
		if !strings.Contains(text, want) {
			t.Errorf("ci.yml lacks %q: the required context must stay ci-complete", want)
		}
	}
}

// TestCallerPinsToolVersions keeps the retired toolchain frozen: every
// version input carries the exact retired pin, so a drift back to a
// floating reusable default turns this red.
func TestCallerPinsToolVersions(t *testing.T) {
	t.Parallel()
	text := readCIYAML(t)
	for _, want := range []string{
		"golangci-lint-version: v2.12.2",
		"govulncheck-version: v1.9.0",
		"gosec-version: v2.30.0",
		"actionlint-version: 1.7.12",
		"actionlint-sha256: 8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("ci.yml lacks %q: tool versions must stay pinned", want)
		}
	}
}

// TestCallerFloorStartsAtZero keeps the ratchet honest: the floor starts at
// 0 until #2 measures the first real baseline, and may only rise.
func TestCallerFloorStartsAtZero(t *testing.T) {
	t.Parallel()
	if text := readCIYAML(t); !strings.Contains(text, "coverage-floor: 0") {
		t.Error("ci.yml lacks `coverage-floor: 0`: the floor must start at zero")
	}
}

// TestImageLaneWaitsOnDockerfile keeps the #3 tripwire semantics: with no
// Dockerfile in the tree, the image gate follows the image filter only. A
// non-PR short-circuit here would run a bare build with nothing to build
// and fail every push and sweep.
func TestImageLaneWaitsOnDockerfile(t *testing.T) {
	t.Parallel()
	want := "run-image: ${{ needs.changes.outputs.image == 'true' }}"
	if text := readCIYAML(t); !strings.Contains(text, want) {
		t.Errorf("ci.yml lacks %q: run-image must stay filter-only until #3 lands", want)
	}
}

// retiredGates pins every lane-selection gate to the exact expression the
// retired inline lanes used. A proof test must fail when the behavior it
// claims breaks: flipping any gate to false (or widening it) turns this
// red, so lane selection cannot go dark unnoticed.
var retiredGates = map[string]string{
	"run-lint":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-test":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.fixtures == 'true' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-race":        "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.misc == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-shell":       "${{ github.event_name != 'pull_request' || needs.changes.outputs.shell == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-boundary":    "false",
	"run-govulncheck": "${{ github.event_name != 'pull_request' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-gosec":       "${{ github.event_name != 'pull_request' || needs.changes.outputs.go == 'true' || needs.changes.outputs.deps == 'true' || needs.changes.outputs.workflow == 'true' }}",
	"run-actionlint":  "${{ github.event_name != 'pull_request' || needs.changes.outputs.workflow == 'true' }}",
}

// TestLaneGatesMirrorRetiredConditions keeps lane selection honest across
// the move: every gate carries the exact retired expression, so disabling
// or widening any lane fails this test.
func TestLaneGatesMirrorRetiredConditions(t *testing.T) {
	t.Parallel()
	text := readCIYAML(t)
	for _, input := range []string{"run-lint", "run-test", "run-race", "run-shell", "run-boundary", "run-govulncheck", "run-gosec", "run-actionlint"} {
		want := input + ": " + retiredGates[input]
		if !strings.Contains(text, want) {
			t.Errorf("ci.yml gate drift for %q: want the exact retired line %q", input, want)
		}
	}
}

// TestAggregatorFailsRed keeps the aggregator honest: only success and
// skipped lanes pass ci-complete, so failing or cancelled lanes fail it.
func TestAggregatorFailsRed(t *testing.T) {
	t.Parallel()
	text := readCIYAML(t)
	for _, want := range []string{`('success', 'skipped')`, "sys.exit(1)"} {
		if !strings.Contains(text, want) {
			t.Errorf("ci-complete aggregator lacks %s", want)
		}
	}
}
