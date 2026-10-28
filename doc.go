// Package collector is the root of the Eshu Azure cloud collector module.
//
// The collector port itself lands under eshu-hq/eshu#8876; this file holds
// the module root so the CI gates (vet, lint, test, race, coverage) have a
// package to judge from the first commit. It carries no behavior.
package collector
