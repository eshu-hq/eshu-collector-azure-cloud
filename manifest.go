// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	_ "embed"
	"fmt"

	sdk "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu-sdk/collector/conformance"
	"gopkg.in/yaml.v3"
)

//go:embed manifest.yaml
var manifestYAML []byte

// ManifestDocument decodes the shipped component manifest. It is the single
// source of truth the binary and the suite both derive the host contract
// from, so the validated output can never drift from the declared manifest.
func ManifestDocument() (conformance.Manifest, error) {
	var manifest conformance.Manifest
	if err := yaml.Unmarshal(manifestYAML, &manifest); err != nil {
		return conformance.Manifest{}, fmt.Errorf("embedded manifest.yaml is invalid: %w", err)
	}
	return manifest, nil
}

// ManifestContract derives the SDK host contract from the shipped component
// manifest. The binary validates its own output against this contract before
// writing it to stdout.
func ManifestContract() sdk.Contract {
	manifest, err := ManifestDocument()
	if err != nil {
		panic(err.Error())
	}
	return manifest.Contract()
}
