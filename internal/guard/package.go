package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var pypiVersionSplitPattern = regexp.MustCompile(`(===|==|~=|!=|<=|>=|<|>)`)

func (s *Server) checkPackage(ctx context.Context, req PackageCheckRequest) PackageCheckResponse {
	if req.MinAgeHours <= 0 {
		req.MinAgeHours = s.cfg.PackageMinAgeHours
	}
	if !req.RequireVerifiedAttestation {
		req.RequireVerifiedAttestation = s.cfg.RequireVerifiedAttestation
	}
	if len(req.Packages) == 0 && strings.TrimSpace(req.Command) != "" {
		req.Packages = ExtractPackageSpecsFromCommand(req.Command)
	}

	cacheKey := hashKey("package", req.Command, stringifyJSON(req.Packages), strings.Join(req.AllowPackages, ","), fmt.Sprintf("%d", req.MinAgeHours), fmt.Sprintf("%t", req.RequireVerifiedAttestation))
	if cached, ok := s.packageCache.get(cacheKey); ok {
		return cached
	}

	resp := PackageCheckResponse{
		Allowed: true,
	}
	if len(req.Packages) == 0 {
		s.packageCache.set(cacheKey, resp)
		return resp
	}

	for _, spec := range req.Packages {
		finding := s.evaluatePackage(ctx, spec, req)
		resp.Findings = append(resp.Findings, finding)
		if len(finding.Reasons) > 0 {
			resp.Allowed = false
			resp.Reasons = append(resp.Reasons, fmt.Sprintf("%s(%s): %s", finding.Manager, finding.Name, strings.Join(finding.Reasons, "; ")))
		}
	}
	resp.Reasons = uniqueStrings(resp.Reasons)
	s.packageCache.set(cacheKey, resp)
	return resp
}

func (s *Server) evaluatePackage(ctx context.Context, spec PackageSpec, req PackageCheckRequest) PackageFinding {
	finding := PackageFinding{
		Manager: strings.ToLower(strings.TrimSpace(spec.Manager)),
		Name:    strings.TrimSpace(spec.Name),
		Version: strings.TrimSpace(spec.Version),
	}
	if finding.Manager == "" {
		finding.Manager = "unknown"
	}
	if finding.Name == "" {
		finding.Reasons = append(finding.Reasons, "missing package name")
		return finding
	}
	if packageMatchesAllowlist(finding.Name, req.AllowPackages) {
		finding.Exists = true
		return finding
	}

	system, canonicalName := normalizePackageIdentity(finding.Manager, finding.Name)
	finding.Name = canonicalName
	if system == "" {
		finding.Reasons = append(finding.Reasons, "unsupported package manager")
		return finding
	}

	registryMeta, regErr := s.lookupRegistryPackage(ctx, system, canonicalName)
	if regErr != nil {
		finding.Reasons = append(finding.Reasons, regErr.Error())
	}
	finding.Exists = registryMeta.exists
	finding.DefaultVersion = registryMeta.defaultVersion
	if finding.Version == "" {
		finding.Version = registryMeta.defaultVersion
	}

	depsPkg, depsPkgErr := s.lookupDepsPackage(ctx, system, canonicalName)
	if depsPkgErr != nil {
		finding.Reasons = append(finding.Reasons, depsPkgErr.Error())
	} else if finding.DefaultVersion == "" {
		finding.DefaultVersion = depsPkg.defaultVersion
		if finding.Version == "" {
			finding.Version = depsPkg.defaultVersion
		}
	}

	if !finding.Exists {
		finding.Reasons = append(finding.Reasons, "package does not exist in registry")
	}

	if finding.Version == "" {
		finding.Reasons = append(finding.Reasons, "unable to resolve package version")
	}

	if similars, err := s.lookupDepsSimilarPackages(ctx, system, canonicalName); err == nil {
		finding.SimilarPackages = similars
	} else {
		finding.Reasons = append(finding.Reasons, err.Error())
	}

	if finding.Version != "" {
		versionMeta, err := s.lookupDepsVersion(ctx, system, canonicalName, finding.Version)
		if err != nil {
			finding.Reasons = append(finding.Reasons, err.Error())
		} else {
			if !versionMeta.publishedAt.IsZero() {
				finding.PublishAgeHours = int(time.Since(versionMeta.publishedAt).Hours())
			}
			finding.VerifiedAttestation = versionMeta.verifiedAttestation
			if versionMeta.hasAdvisories {
				finding.Reasons = append(finding.Reasons, "package version has published security advisories")
			}
			if versionMeta.isDeprecated {
				finding.Reasons = append(finding.Reasons, "package version is deprecated")
			}
		}
	}

	if finding.PublishAgeHours > 0 && finding.PublishAgeHours < req.MinAgeHours {
		finding.Reasons = append(finding.Reasons, fmt.Sprintf("package is too new (%dh < %dh minimum age)", finding.PublishAgeHours, req.MinAgeHours))
	}
	if req.RequireVerifiedAttestation && !finding.VerifiedAttestation {
		finding.Reasons = append(finding.Reasons, "package lacks a verified attestation")
	}
	if len(finding.SimilarPackages) > 0 && (!finding.Exists || finding.PublishAgeHours < req.MinAgeHours || !finding.VerifiedAttestation) {
		finding.Reasons = append(finding.Reasons, "similar package names exist: "+strings.Join(finding.SimilarPackages, ", "))
	}

	// OSV malicious-packages check
	if s.cfg.OSVEnabled && finding.Exists {
		if malicious, malReasons := s.lookupOSVMalicious(ctx, finding.Manager, canonicalName, finding.Version); malicious {
			finding.Reasons = append(finding.Reasons, malReasons...)
		}
	}

	// GuardDog heuristic scan
	if s.cfg.GuardDogEnabled && finding.Exists {
		if flagged, gdReasons := s.runGuardDog(ctx, finding.Manager, canonicalName); flagged {
			finding.Reasons = append(finding.Reasons, gdReasons...)
		}
	}

	finding.Reasons = uniqueStrings(finding.Reasons)
	return finding
}

type registryPackageMeta struct {
	exists         bool
	defaultVersion string
}

type depsPackageMeta struct {
	defaultVersion string
}

type depsVersionMeta struct {
	publishedAt         time.Time
	verifiedAttestation bool
	hasAdvisories       bool
	isDeprecated        bool
}

func (s *Server) lookupRegistryPackage(ctx context.Context, system string, name string) (registryPackageMeta, error) {
	switch system {
	case "npm":
		return s.lookupNPMRegistry(ctx, name)
	case "pypi":
		return s.lookupPyPIRegistry(ctx, name)
	default:
		return registryPackageMeta{}, fmt.Errorf("unsupported package system %q", system)
	}
}

func (s *Server) lookupNPMRegistry(ctx context.Context, name string) (registryPackageMeta, error) {
	endpoint := "https://registry.npmjs.org/" + url.PathEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return registryPackageMeta{}, fmt.Errorf("npm registry lookup failed")
	}
	req.Header.Set("User-Agent", "glass-guardd/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return registryPackageMeta{}, fmt.Errorf("npm registry lookup failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return registryPackageMeta{}, nil
	}
	if resp.StatusCode >= 400 {
		return registryPackageMeta{}, fmt.Errorf("npm registry lookup failed")
	}

	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return registryPackageMeta{}, fmt.Errorf("npm registry response unreadable")
	}
	return registryPackageMeta{
		exists:         true,
		defaultVersion: payload.DistTags["latest"],
	}, nil
}

func (s *Server) lookupPyPIRegistry(ctx context.Context, name string) (registryPackageMeta, error) {
	endpoint := "https://pypi.org/pypi/" + url.PathEscape(normalizePyPIName(name)) + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return registryPackageMeta{}, fmt.Errorf("PyPI lookup failed")
	}
	req.Header.Set("User-Agent", "glass-guardd/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return registryPackageMeta{}, fmt.Errorf("PyPI lookup failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return registryPackageMeta{}, nil
	}
	if resp.StatusCode >= 400 {
		return registryPackageMeta{}, fmt.Errorf("PyPI lookup failed")
	}

	var payload struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return registryPackageMeta{}, fmt.Errorf("PyPI response unreadable")
	}
	return registryPackageMeta{
		exists:         true,
		defaultVersion: payload.Info.Version,
	}, nil
}

func (s *Server) lookupDepsPackage(ctx context.Context, system string, name string) (depsPackageMeta, error) {
	endpoint := fmt.Sprintf("https://api.deps.dev/v3alpha/systems/%s/packages/%s", system, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return depsPackageMeta{}, fmt.Errorf("deps.dev package lookup failed")
	}
	req.Header.Set("User-Agent", "glass-guardd/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return depsPackageMeta{}, fmt.Errorf("deps.dev package lookup failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return depsPackageMeta{}, fmt.Errorf("deps.dev package lookup failed")
	}

	var payload struct {
		Versions []struct {
			VersionKey struct {
				Version string `json:"version"`
			} `json:"versionKey"`
			IsDefault bool `json:"isDefault"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 3<<20)).Decode(&payload); err != nil {
		return depsPackageMeta{}, fmt.Errorf("deps.dev package response unreadable")
	}
	meta := depsPackageMeta{}
	for _, version := range payload.Versions {
		if version.IsDefault {
			meta.defaultVersion = version.VersionKey.Version
			break
		}
	}
	return meta, nil
}

func (s *Server) lookupDepsVersion(ctx context.Context, system string, name string, version string) (depsVersionMeta, error) {
	endpoint := fmt.Sprintf("https://api.deps.dev/v3alpha/systems/%s/packages/%s/versions/%s", system, url.PathEscape(name), url.PathEscape(version))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return depsVersionMeta{}, fmt.Errorf("deps.dev version lookup failed")
	}
	req.Header.Set("User-Agent", "glass-guardd/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return depsVersionMeta{}, fmt.Errorf("deps.dev version lookup failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return depsVersionMeta{}, fmt.Errorf("deps.dev version lookup failed")
	}

	var payload struct {
		PublishedAt  string     `json:"publishedAt"`
		IsDeprecated bool       `json:"isDeprecated"`
		AdvisoryKeys []struct{} `json:"advisoryKeys"`
		SLSAProv     []struct {
			Verified bool `json:"verified"`
		} `json:"slsaProvenances"`
		Attestations []struct {
			Verified bool `json:"verified"`
		} `json:"attestations"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return depsVersionMeta{}, fmt.Errorf("deps.dev version response unreadable")
	}

	meta := depsVersionMeta{
		hasAdvisories: len(payload.AdvisoryKeys) > 0,
		isDeprecated:  payload.IsDeprecated,
	}
	if payload.PublishedAt != "" {
		if ts, err := time.Parse(time.RFC3339, payload.PublishedAt); err == nil {
			meta.publishedAt = ts
		}
	}
	for _, att := range payload.Attestations {
		if att.Verified {
			meta.verifiedAttestation = true
			break
		}
	}
	if !meta.verifiedAttestation {
		for _, prov := range payload.SLSAProv {
			if prov.Verified {
				meta.verifiedAttestation = true
				break
			}
		}
	}
	return meta, nil
}

func (s *Server) lookupDepsSimilarPackages(ctx context.Context, system string, name string) ([]string, error) {
	endpoint := fmt.Sprintf("https://api.deps.dev/v3alpha/systems/%s/packages/%s:similarlyNamedPackages", system, url.PathEscape(name))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("deps.dev similar-name lookup failed")
	}
	req.Header.Set("User-Agent", "glass-guardd/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deps.dev similar-name lookup failed")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("deps.dev similar-name lookup failed")
	}

	var payload struct {
		Packages []struct {
			PackageKey struct {
				Name string `json:"name"`
			} `json:"packageKey"`
		} `json:"packages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("deps.dev similar-name response unreadable")
	}

	var out []string
	for _, pkg := range payload.Packages {
		if pkg.PackageKey.Name == "" || pkg.PackageKey.Name == name {
			continue
		}
		out = append(out, pkg.PackageKey.Name)
		if len(out) == 5 {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

func packageMatchesAllowlist(name string, allowPackages []string) bool {
	for _, allowed := range allowPackages {
		if strings.EqualFold(strings.TrimSpace(allowed), name) {
			return true
		}
	}
	return false
}

func normalizePackageIdentity(manager string, name string) (system string, canonicalName string) {
	manager = strings.ToLower(strings.TrimSpace(manager))
	name = strings.TrimSpace(name)
	switch manager {
	case "npm", "pnpm", "yarn", "bun", "npx":
		return "npm", name
	case "pip", "pip3", "poetry", "uv":
		return "pypi", normalizePyPIName(name)
	default:
		return "", name
	}
}

func normalizePyPIName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	replacer := strings.NewReplacer("_", "-", ".", "-")
	name = replacer.Replace(name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return name
}

func ExtractPackageSpecsFromCommand(command string) []PackageSpec {
	var out []PackageSpec
	seen := make(map[string]struct{})
	for _, segment := range splitShellSegments(command) {
		tokens := splitShellWordsLoose(segment)
		specs := extractPackageSpecsFromTokens(tokens)
		for _, spec := range specs {
			key := strings.ToLower(spec.Manager) + "\x00" + strings.ToLower(spec.Name) + "\x00" + spec.Version
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, spec)
		}
	}
	return out
}

func splitShellSegments(command string) []string {
	var segments []string
	var buf strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		part := strings.TrimSpace(buf.String())
		if part != "" {
			segments = append(segments, part)
		}
		buf.Reset()
	}

	for _, r := range command {
		if escaped {
			buf.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			buf.WriteRune(r)
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			buf.WriteRune(r)
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			buf.WriteRune(r)
			continue
		}
		if r == ';' || r == '\n' || r == '&' || r == '|' {
			flush()
			continue
		}
		buf.WriteRune(r)
	}
	flush()
	return segments
}

func splitShellWordsLoose(segment string) []string {
	var words []string
	var buf strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		words = append(words, buf.String())
		buf.Reset()
	}

	for _, r := range segment {
		if escaped {
			buf.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				buf.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' {
			flush()
			continue
		}
		buf.WriteRune(r)
	}
	flush()
	return words
}

func extractPackageSpecsFromTokens(tokens []string) []PackageSpec {
	if len(tokens) == 0 {
		return nil
	}

	for len(tokens) > 0 && (tokens[0] == "sudo" || tokens[0] == "env" || strings.HasPrefix(tokens[0], "VAR=")) {
		tokens = tokens[1:]
	}
	if len(tokens) == 0 {
		return nil
	}

	cmd := strings.ToLower(tokens[0])
	switch cmd {
	case "npm", "pnpm", "bun":
		if len(tokens) < 3 {
			return nil
		}
		action := strings.ToLower(tokens[1])
		if action != "install" && action != "i" && action != "add" {
			return nil
		}
		return parseNPMPackages(cmd, tokens[2:])
	case "yarn":
		if len(tokens) < 3 {
			return nil
		}
		if strings.ToLower(tokens[1]) != "add" {
			return nil
		}
		return parseNPMPackages(cmd, tokens[2:])
	case "npx":
		if len(tokens) < 2 {
			return nil
		}
		pkg := firstPackageArg(tokens[1:])
		if pkg == "" {
			return nil
		}
		name, version := splitNPMPackageToken(pkg)
		return []PackageSpec{{Manager: "npx", Name: name, Version: version}}
	case "pip", "pip3":
		if len(tokens) < 3 || strings.ToLower(tokens[1]) != "install" {
			return nil
		}
		return parsePyPIPackages(cmd, tokens[2:])
	case "python", "python3":
		if len(tokens) >= 5 && tokens[1] == "-m" && strings.ToLower(tokens[2]) == "pip" && strings.ToLower(tokens[3]) == "install" {
			return parsePyPIPackages("pip", tokens[4:])
		}
	case "poetry":
		if len(tokens) < 3 || strings.ToLower(tokens[1]) != "add" {
			return nil
		}
		return parsePyPIPackages(cmd, tokens[2:])
	case "uv":
		if len(tokens) >= 3 && strings.ToLower(tokens[1]) == "add" {
			return parsePyPIPackages(cmd, tokens[2:])
		}
		if len(tokens) >= 5 && strings.ToLower(tokens[1]) == "pip" && strings.ToLower(tokens[2]) == "install" {
			return parsePyPIPackages(cmd, tokens[3:])
		}
	}

	return nil
}

func parseNPMPackages(manager string, args []string) []PackageSpec {
	var out []PackageSpec
	for _, arg := range args {
		if skipPackageArg(arg) {
			continue
		}
		name, version := splitNPMPackageToken(arg)
		if name == "" {
			continue
		}
		out = append(out, PackageSpec{
			Manager: manager,
			Name:    name,
			Version: version,
		})
	}
	return out
}

func parsePyPIPackages(manager string, args []string) []PackageSpec {
	var out []PackageSpec
	for _, arg := range args {
		if skipPackageArg(arg) {
			continue
		}
		name, version := splitPyPIPackageToken(arg)
		if name == "" {
			continue
		}
		out = append(out, PackageSpec{
			Manager: manager,
			Name:    name,
			Version: version,
		})
	}
	return out
}

func firstPackageArg(args []string) string {
	for _, arg := range args {
		if skipPackageArg(arg) {
			continue
		}
		return arg
	}
	return ""
}

func skipPackageArg(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.HasPrefix(arg, "-") {
		return true
	}
	lower := strings.ToLower(arg)
	return strings.HasPrefix(lower, "git+") ||
		strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "file:") ||
		strings.HasPrefix(lower, "./") ||
		strings.HasPrefix(lower, "../") ||
		strings.HasPrefix(lower, "/") ||
		lower == "."
}

func splitNPMPackageToken(token string) (string, string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ""
	}
	if strings.HasPrefix(token, "@") {
		lastAt := strings.LastIndex(token, "@")
		if lastAt > 0 {
			name := token[:lastAt]
			version := token[lastAt+1:]
			if strings.Contains(name, "/") && version != "" {
				return name, version
			}
		}
		return token, ""
	}
	if idx := strings.LastIndex(token, "@"); idx > 0 {
		return token[:idx], token[idx+1:]
	}
	return token, ""
}

func splitPyPIPackageToken(token string) (string, string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ""
	}
	name := token
	version := ""
	if match := pypiVersionSplitPattern.FindStringIndex(token); match != nil {
		name = token[:match[0]]
		version = token[match[1]:]
	}
	if idx := strings.Index(name, "["); idx >= 0 {
		name = name[:idx]
	}
	return normalizePyPIName(name), strings.TrimSpace(version)
}
