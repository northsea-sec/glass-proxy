package guard

import "time"

const (
	DefaultBindAddr         = "127.0.0.1:18900"
	DefaultBaseURL          = "http://127.0.0.1:18900"
	DefaultPackageMinAgeHrs = 168
	DefaultCacheTTL         = time.Hour
)

type URLCheckRequest struct {
	URL        string   `json:"url"`
	AllowHosts []string `json:"allow_hosts,omitempty"`
}

type URLCheckResponse struct {
	Allowed       bool     `json:"allowed"`
	NormalizedURL string   `json:"normalized_url,omitempty"`
	Host          string   `json:"host,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
}

type ContentCheckRequest struct {
	SourceURL   string `json:"source_url,omitempty"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

type ContentCheckResponse struct {
	Allowed       bool     `json:"allowed"`
	Reasons       []string `json:"reasons,omitempty"`
	SanitizedText string   `json:"sanitized_text,omitempty"`
	URLs          []string `json:"urls,omitempty"`
}

type PackageSpec struct {
	Manager string `json:"manager"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type PackageCheckRequest struct {
	Command                    string        `json:"command,omitempty"`
	Packages                   []PackageSpec `json:"packages"`
	AllowPackages              []string      `json:"allow_packages,omitempty"`
	MinAgeHours                int           `json:"min_age_hours"`
	RequireVerifiedAttestation bool          `json:"require_verified_attestation"`
}

type PackageFinding struct {
	Manager             string   `json:"manager"`
	Name                string   `json:"name"`
	Version             string   `json:"version,omitempty"`
	Exists              bool     `json:"exists"`
	DefaultVersion      string   `json:"default_version,omitempty"`
	PublishAgeHours     int      `json:"publish_age_hours,omitempty"`
	VerifiedAttestation bool     `json:"verified_attestation"`
	SimilarPackages     []string `json:"similar_packages,omitempty"`
	Reasons             []string `json:"reasons,omitempty"`
}

type PackageCheckResponse struct {
	Allowed  bool             `json:"allowed"`
	Reasons  []string         `json:"reasons,omitempty"`
	Findings []PackageFinding `json:"findings,omitempty"`
}

type ServerConfig struct {
	BindAddr                   string
	URLhausAuthKey             string
	PhishTankAppKey            string
	PackageMinAgeHours         int
	RequireVerifiedAttestation bool
	CacheTTL                   time.Duration

	// Extended scanners (GLASSDD)
	OSVEnabled        bool
	GuardDogEnabled   bool
	DnstwistEnabled   bool
	JSXRayEnabled     bool
	LLMGuardEnabled   bool
	LLMGuardBind      string
	ProtectedDomains  []string
	VenvPath          string
}
