package trim

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"chainguard.dev/apko/pkg/apk/apk"
)

// DependencyResolver wraps APK index data to resolve package dependencies
type DependencyResolver struct {
	// pkgIndex maps package name -> Package for latest version of each package
	pkgIndex map[string]*apk.Package
	// nameProviders maps a name (package name or provides) -> list of packages that provide it
	nameProviders map[string][]*apk.Package
	// transitiveDepsCache caches computed transitive dependencies to avoid recomputation
	transitiveDepsCache map[string]map[string]bool
}

// NewResolver creates a new DependencyResolver from repository indexes
func NewResolver(ctx context.Context, repos []string, keys map[string][]byte, arch string) (*DependencyResolver, error) {
	if len(repos) == 0 {
		return nil, fmt.Errorf("no repositories specified")
	}

	opts := []apk.IndexOption{
		apk.WithIgnoreSignatures(true), // For trim we can skip signature verification
		apk.WithHTTPClient(http.DefaultClient),
	}

	indexes, err := apk.GetRepositoryIndexes(ctx, repos, keys, arch, opts...)
	if err != nil {
		return nil, fmt.Errorf("fetching repository indexes: %w", err)
	}

	return newResolverFromIndexes(indexes), nil
}

// newResolverFromIndexes creates a resolver from already fetched indexes
func newResolverFromIndexes(indexes []apk.NamedIndex) *DependencyResolver {
	pkgIndex := make(map[string]*apk.Package)
	nameProviders := make(map[string][]*apk.Package)

	for _, idx := range indexes {
		for _, repoPkg := range idx.Packages() {
			pkg := repoPkg.Package // repoPkg embeds *Package

			// Store the package by name (keep latest version)
			if existing, ok := pkgIndex[pkg.Name]; !ok || compareVersions(pkg.Version, existing.Version) > 0 {
				pkgIndex[pkg.Name] = pkg
			}

			// Map package name to this package
			nameProviders[pkg.Name] = append(nameProviders[pkg.Name], pkg)

			// Map each "provides" entry to this package
			for _, prov := range pkg.Provides {
				provName := apk.ResolvePackageNameVersionPin(prov).Name
				nameProviders[provName] = append(nameProviders[provName], pkg)
			}
		}
	}

	return &DependencyResolver{
		pkgIndex:            pkgIndex,
		nameProviders:       nameProviders,
		transitiveDepsCache: make(map[string]map[string]bool),
	}
}

// GetDependencies returns the direct dependencies of a package
func (r *DependencyResolver) GetDependencies(name string) []string {
	pkg, ok := r.pkgIndex[name]
	if !ok {
		return nil
	}
	return pkg.Dependencies
}

// GetProvides returns what a package provides
func (r *DependencyResolver) GetProvides(name string) []string {
	pkg, ok := r.pkgIndex[name]
	if !ok {
		return nil
	}
	return pkg.Provides
}

// GetTransitiveDeps returns all transitive dependencies of a package.
// Results are cached to avoid repeated computation.
func (r *DependencyResolver) GetTransitiveDeps(name string) map[string]bool {
	if cached, ok := r.transitiveDepsCache[name]; ok {
		return cached
	}

	visited := make(map[string]bool)
	r.collectDeps(name, visited)
	delete(visited, name) // Don't include the package itself

	r.transitiveDepsCache[name] = visited
	return visited
}

// collectDeps recursively collects all dependencies
func (r *DependencyResolver) collectDeps(name string, visited map[string]bool) {
	if visited[name] {
		return
	}
	visited[name] = true

	deps := r.GetDependencies(name)
	for _, dep := range deps {
		// Skip conflict markers
		if strings.HasPrefix(dep, "!") {
			continue
		}

		// Parse dependency to get just the package name
		depName := parseDependency(dep)
		if depName == "" {
			continue
		}

		// Skip virtual provides (so:*, cmd:*, pc:*, etc.)
		// These can be provided by multiple packages and following them
		// leads to incorrect dependency chains
		if isVirtualProvide(depName) {
			continue
		}

		// Only follow direct package dependencies
		if _, ok := r.pkgIndex[depName]; ok {
			r.collectDeps(depName, visited)
		}
	}
}

// isVirtualProvide checks if a dependency name is a virtual provide
// (not a real package name)
func isVirtualProvide(name string) bool {
	// Virtual provide prefixes used in Wolfi/Alpine APK
	// Verified against actual APKINDEX data
	return strings.HasPrefix(name, "so:") || // shared libraries (e.g., so:libc.musl-x86_64.so.1)
		strings.HasPrefix(name, "cmd:") || // commands/executables (e.g., cmd:python3)
		strings.HasPrefix(name, "pc:") // pkg-config files (e.g., pc:openssl)
}

// IsTransitiveDep checks if pkgA is a transitive dependency of pkgB
func (r *DependencyResolver) IsTransitiveDep(pkgA, pkgB string) bool {
	transDeps := r.GetTransitiveDeps(pkgB)
	if transDeps[pkgA] {
		return true
	}
	// Also check if pkgA is provided by any transitive dep
	for depName := range transDeps {
		provides := r.GetProvides(depName)
		for _, prov := range provides {
			if apk.ResolvePackageNameVersionPin(prov).Name == pkgA {
				return true
			}
		}
	}
	return false
}

// parseDependency extracts the package name from a dependency string like "foo>=1.0"
// It handles special cases that apk.ResolvePackageNameVersionPin doesn't:
// - Conflict markers (!foo) return empty string
// - Pinned deps (@pinname:foo) strip the pin prefix
func parseDependency(dep string) string {
	// Skip conflict markers
	if strings.HasPrefix(dep, "!") {
		return ""
	}
	// Handle pinned deps like @pinname:foo
	if strings.HasPrefix(dep, "@") {
		if idx := strings.Index(dep, ":"); idx > 0 {
			dep = dep[idx+1:]
		}
	}
	return apk.ResolvePackageNameVersionPin(dep).Name
}

// compareVersions compares two APK version strings
// Returns positive if a > b, negative if a < b, zero if equal
func compareVersions(a, b string) int {
	verA, errA := apk.ParseVersion(a)
	verB, errB := apk.ParseVersion(b)

	// If either fails to parse, fall back to string comparison
	if errA != nil || errB != nil {
		if a > b {
			return 1
		}
		if a < b {
			return -1
		}
		return 0
	}

	return apk.CompareVersions(verA, verB)
}
