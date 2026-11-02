// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

// Command collector is the standalone Eshu Azure cloud collector satellite
// binary. It observes Azure provider truth from fixtures and emits versioned
// source facts through the published SDK stdio contract
// (collector-sdk/v1alpha1).
//
// It runs in two modes:
//
//   - --sdk-stdio (host protocol): reads one collector SDK host request from
//     stdin, collects the claim-authorized scope, and writes one SDK Result to
//     stdout. The host owns admission, identity, projection, persistence, and
//     recovery; this binary never commits facts or writes graph truth.
//   - local fixture flags (default): collects the configured fixture pages for
//     one synthesized fixture claim and writes the SDK Result to stdout. This
//     is the fixture proof path; it issues no live Azure call.
//
// Fixture mode is the default. Live transport stays behind an explicit opt-in
// (--live, or "live": true in a stdio request) and the gated live seam, which
// fails closed until a read-only adapter is injected. No default code path and
// no test issues a live Azure request. Credentials are referenced by NAME
// only; a credential value or name is never read from config and never logged.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	collector "github.com/eshu-hq/eshu-collector-azure-cloud"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("collector", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pages := flags.String("pages", "testdata/resources_page1.json,testdata/resources_page2.json", "comma-separated fixture page paths")
	targets := flags.String("targets", "", "JSON array of bounded Azure scope shards (default: the parity fixture target)")
	instanceID := flags.String("collector-instance-id", "azure-instance-1", "collector instance that owns target policy")
	generationID := flags.String("generation-id", "", "durable generation id (default: derived per sweep)")
	redactionKeyFile := flags.String("redaction-key-file", "", "path to the read-only redaction key material file (enables tag/identity/DNS/image emission)")
	live := flags.Bool("live", false, "opt in to the live provider seam (fails closed while no live adapter is injected)")
	maxRecords := flags.Int("max-records", 0, "max records per claim (0 selects the default bound)")
	maxPayloadBytes := flags.Int("max-payload-bytes", 0, "max payload bytes per fact (0 selects the default bound)")
	claimTimeoutSeconds := flags.Int("claim-timeout-seconds", 0, "claim timeout in seconds (0 selects the default bound)")
	sdkStdio := flags.Bool("sdk-stdio", false, "read one collector SDK host request from stdin")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *maxRecords < 0 || *maxPayloadBytes < 0 || *claimTimeoutSeconds < 0 {
		return fmt.Errorf("limits must be non-negative (0 selects defaults)")
	}
	limits := collector.DefaultResourceUse()
	if *maxRecords > 0 {
		limits.MaxRecordsPerClaim = *maxRecords
	}
	if *maxPayloadBytes > 0 {
		limits.MaxPayloadBytes = *maxPayloadBytes
	}
	if *claimTimeoutSeconds > 0 {
		limits.ClaimTimeoutSeconds = *claimTimeoutSeconds
	}

	monitor := collector.NewMonitor()
	monitor.SetResource(limits)

	var result resultWithMeta
	var err error
	if *sdkStdio {
		var plan stdioPlan
		plan, err = parseSDKStdio(stdin)
		if err == nil {
			limits = plan.limits
			monitor.SetResource(limits)
			// Only a fixture run carries the fixture-only label; a live run
			// leaves it false so the health line never mislabels live reads.
			if !plan.live {
				monitor.MarkFixtureOnly()
			}
			result, err = runWithTimeout(func() (resultWithMeta, error) {
				return collectStdioPlan(plan)
			}, limits)
		}
	} else {
		opts := localOptions{
			pages:            splitPages(*pages),
			targetsJSON:      strings.TrimSpace(*targets),
			collectorID:      strings.TrimSpace(*instanceID),
			generationID:     strings.TrimSpace(*generationID),
			redactionKeyFile: strings.TrimSpace(*redactionKeyFile),
			live:             *live,
		}
		if !opts.live {
			monitor.MarkFixtureOnly()
		}
		result, err = runWithTimeout(func() (resultWithMeta, error) {
			return collectLocal(opts, limits)
		}, limits)
	}
	monitor.ObserveCounts(result.factCount, result.warningCount)
	if err != nil {
		monitor.ObserveFailure(firstLine(err.Error()))
		health, resource := monitor.Snapshot()
		logHealth(stderr, health, resource)
		return err
	}
	// Only a completed collection moves the success digest; terminal outcomes
	// observe as failures and unchanged outcomes leave the last good digest
	// alone.
	if result.digest != "" {
		monitor.ObserveSuccess(result.digest, time.Now().UTC())
	}
	health, resource := monitor.Snapshot()
	logHealth(stderr, health, resource)
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result.result)
}

// logHealth writes the operator health snapshot to stderr. Error and digest
// strings here never carry credentials: collection errors never echo request
// config, URIs, or payloads, and digests are opaque hashes.
func logHealth(stderr io.Writer, health collector.Health, resource collector.ResourceUse) {
	line, err := json.Marshal(map[string]any{"health": health, "resource": resource})
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(stderr, "collector health: %s\n", line)
}

func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func splitPages(raw string) []string {
	var out []string
	for _, page := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(page); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
