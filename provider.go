// SPDX-License-Identifier: MIT
// Copyright (c) 2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PageProviderFactory builds the PageProvider that serves Resource Graph pages
// for one bounded scope target. It is the single seam that keeps live Azure
// transport out of the runtime and tests: fixtures supply a file-backed
// provider, while an explicitly injected live adapter serves production reads.
// Implementations MUST be read-only.
type PageProviderFactory interface {
	// PageProvider returns the provider for the given boundary and target. The
	// returned provider performs read-only inventory queries only.
	PageProvider(ctx context.Context, boundary Boundary, target TargetConfig) (PageProvider, error)
}

// PageProviderFactoryFunc adapts a function into a PageProviderFactory.
type PageProviderFactoryFunc func(context.Context, Boundary, TargetConfig) (PageProvider, error)

// PageProvider calls the adapted function.
func (f PageProviderFactoryFunc) PageProvider(
	ctx context.Context,
	boundary Boundary,
	target TargetConfig,
) (PageProvider, error) {
	return f(ctx, boundary, target)
}

// FixturePageProvider serves pre-parsed Resource Graph pages keyed by
// $skipToken, modeling provider pagination without live calls. The empty string
// keys the first page. It records partial-scope access as explicit evidence
// rather than silent success. It is the test and offline provider; it issues no
// network calls.
type FixturePageProvider struct {
	mu          sync.Mutex
	pages       map[string]ResourceGraphPage
	changePages map[string]ResourceChangesPage
	access      ScopeAccess
	calls       []string
}

// NewFixturePageProvider builds a FixturePageProvider from skip-token-keyed
// pages and an optional partial-scope access report. The empty-string key is
// the first page; a non-empty $skipToken on a page must key the following page.
func NewFixturePageProvider(
	pages map[string]ResourceGraphPage,
	access ScopeAccess,
) *FixturePageProvider {
	cloned := make(map[string]ResourceGraphPage, len(pages))
	for token, page := range pages {
		cloned[token] = page
	}
	return &FixturePageProvider{pages: cloned, access: access}
}

// NewFixtureResourceChangesPageProvider builds a FixturePageProvider from
// resourcechanges pages. It is fixture-only and never calls Azure.
func NewFixtureResourceChangesPageProvider(
	pages map[string]ResourceChangesPage,
	access ScopeAccess,
) *FixturePageProvider {
	cloned := make(map[string]ResourceChangesPage, len(pages))
	for token, page := range pages {
		cloned[token] = page
	}
	return &FixturePageProvider{changePages: cloned, access: access}
}

// NewFixturePageProviderFromFiles parses one JSON Resource Graph page per file
// path and chains them by each page's $skipToken, starting from the first file
// at the empty token. It is the file-backed offline provider used by smoke
// tests and local tooling; it never calls Azure.
func NewFixturePageProviderFromFiles(
	access ScopeAccess,
	paths ...string,
) (*FixturePageProvider, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one fixture page path is required")
	}
	pages := make(map[string]ResourceGraphPage, len(paths))
	token := ""
	for i, path := range paths {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read azure fixture page %d: %w", i, err)
		}
		page, err := ParseResourceGraphPage(raw)
		if err != nil {
			return nil, fmt.Errorf("parse azure fixture page %d: %w", i, err)
		}
		pages[token] = page
		token = page.SkipToken
	}
	return NewFixturePageProvider(pages, access), nil
}

// NewFixtureResourceChangesPageProviderFromFiles parses one JSON Resource Graph
// resourcechanges page per file path and chains them by each page's $skipToken.
// It is the file-backed offline provider for resource-change smoke tests; it
// never calls Azure.
func NewFixtureResourceChangesPageProviderFromFiles(
	access ScopeAccess,
	paths ...string,
) (*FixturePageProvider, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("at least one fixture resourcechanges page path is required")
	}
	pages := make(map[string]ResourceChangesPage, len(paths))
	token := ""
	for i, path := range paths {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("read azure fixture resourcechanges page %d: %w", i, err)
		}
		page, err := ParseResourceChangesPage(raw)
		if err != nil {
			return nil, fmt.Errorf("parse azure fixture resourcechanges page %d: %w", i, err)
		}
		pages[token] = page
		token = page.SkipToken
	}
	return NewFixtureResourceChangesPageProvider(pages, access), nil
}

// NextPage returns the page for the given $skipToken. A missing token is an
// error rather than an empty success so a fixture gap surfaces explicitly.
func (p *FixturePageProvider) NextPage(
	_ context.Context,
	skipToken string,
) (ResourceGraphPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, skipToken)
	page, ok := p.pages[skipToken]
	if !ok {
		return ResourceGraphPage{}, fmt.Errorf("no azure fixture page for skip token %q", skipToken)
	}
	return page, nil
}

// NextResourceChangesPage returns the resourcechanges page for the given
// $skipToken. A missing token is an error rather than an empty success.
func (p *FixturePageProvider) NextResourceChangesPage(
	_ context.Context,
	skipToken string,
) (ResourceChangesPage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, skipToken)
	page, ok := p.changePages[skipToken]
	if !ok {
		return ResourceChangesPage{}, fmt.Errorf("no azure fixture resourcechanges page for skip token %q", skipToken)
	}
	return page, nil
}

// ScopeAccess reports the configured partial-scope access for the fixture.
func (p *FixturePageProvider) ScopeAccess(context.Context) ScopeAccess {
	return p.access
}

// Calls returns the ordered $skipToken values requested so tests can assert
// pagination and resume behavior.
func (p *FixturePageProvider) Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.calls))
	copy(out, p.calls)
	return out
}

// StaticFixtureFactory returns a PageProviderFactory that always serves the same
// FixturePageProvider, regardless of boundary or target. It is for tests and
// offline runs only.
func StaticFixtureFactory(provider PageProvider) PageProviderFactory {
	return PageProviderFactoryFunc(func(
		context.Context,
		Boundary,
		TargetConfig,
	) (PageProvider, error) {
		if provider == nil {
			return nil, fmt.Errorf("static fixture factory requires a page provider")
		}
		return provider, nil
	})
}
