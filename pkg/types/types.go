package types

import (
	"fmt"
	"time"
)

// Dependency represents a single dependency constraint from dependencies.yaml
type Dependency struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

// DependenciesFile represents the structure of dependencies.yaml
type DependenciesFile struct {
	Dependencies []Dependency `yaml:"dependencies" json:"dependencies"`
}

// Release represents a deployed Helm release
type Release struct {
	Name      string
	Namespace string
	Chart     ChartInfo
	Status    string
	Version   int
	Updated   time.Time
}

// ChartInfo contains information about a Helm chart
type ChartInfo struct {
	Name    string
	Version string
}

// CheckResult represents the result of dependency checking
type CheckResult struct {
	Success           bool               `json:"success"`
	ChartResults      []ChartCheckResult `json:"chart_results"`
	TotalSummary      ResultSummary      `json:"total_summary"`
	Errors            []ValidationError  `json:"errors,omitempty"`
	MatchedNamespaces []string           `json:"matched_namespaces,omitempty"`
}

// ChartCheckResult represents the check result for a single chart
type ChartCheckResult struct {
	ChartPath         string             `json:"chart_path"`
	ChartName         string             `json:"chart_name"`
	Success           bool               `json:"success"`
	Dependencies      []DependencyResult `json:"dependencies"`
	Errors            []ValidationError  `json:"errors,omitempty"`
	Summary           ResultSummary      `json:"summary"`
}

// DependencyResult represents the check result for a single dependency
type DependencyResult struct {
	Name            string    `json:"name"`
	RequiredVersion string    `json:"required_version"`
	Status          string    `json:"status"` // "satisfied", "not_found", "version_mismatch", "multiple_found"
	FoundReleases   []Release `json:"found_releases,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// ResultSummary provides a summary of the check results
type ResultSummary struct {
	Total      int `json:"total"`
	Satisfied  int `json:"satisfied"`
	NotFound   int `json:"not_found"`
	Mismatched int `json:"mismatched"`
	Multiple   int `json:"multiple"`
	Errors     int `json:"errors"`
}

// ValidationError represents different types of validation errors
type ValidationError struct {
	Type    ErrorType    `json:"type"`
	Chart   string       `json:"chart"`
	Message string       `json:"message"`
	Details ErrorDetails `json:"details,omitempty"`
}

// ErrorType defines the type of validation error
type ErrorType string

const (
	ErrorTypeDependencyNotFound       ErrorType = "dependency_not_found"
	ErrorTypeVersionMismatch          ErrorType = "version_mismatch"
	ErrorTypeMultipleDeployments      ErrorType = "multiple_deployments"
	ErrorTypeDuplicateInNamespace     ErrorType = "duplicate_in_namespace"
	ErrorTypeInvalidDependencyFile    ErrorType = "invalid_dependency_file"
	ErrorTypeHelmClientError          ErrorType = "helm_client_error"
	ErrorTypeInvalidVersionConstraint ErrorType = "invalid_version_constraint"
)

// ErrorDetails contains additional context for errors
type ErrorDetails struct {
	RequiredVersion string   `json:"required_version,omitempty"`
	FoundVersion    string   `json:"found_version,omitempty"`
	Namespace       string   `json:"namespace,omitempty"`
	Release         string   `json:"release,omitempty"`
	SearchPattern   string   `json:"search_pattern,omitempty"`
	FoundNamespaces []string `json:"found_namespaces,omitempty"`
	FoundReleases   []string `json:"found_releases,omitempty"`
	File            string   `json:"file,omitempty"`
	Line            int      `json:"line,omitempty"`
}

// Config holds configuration for the dependency checker
type Config struct {
	ChartPaths       []string // Changed from ChartPath to ChartPaths
	NamespacePattern string
	Verbose          bool
	OutputFormat     string
	KubeConfig       string
}

// OutputFormat defines supported output formats
type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
	OutputFormatYAML OutputFormat = "yaml"
)

// Error implementations for ValidationError
func (e ValidationError) Error() string {
	switch e.Type {
	case ErrorTypeDependencyNotFound:
		return fmt.Sprintf("Dependency not found: %s (required: %s, search pattern: %s)",
			e.Chart, e.Details.RequiredVersion, e.Details.SearchPattern)
	case ErrorTypeVersionMismatch:
		return fmt.Sprintf("Version constraint not satisfied: %s (found: %s, required: %s, namespace: %s)",
			e.Chart, e.Details.FoundVersion, e.Details.RequiredVersion, e.Details.Namespace)
	case ErrorTypeMultipleDeployments:
		return fmt.Sprintf("Multiple deployments found: %s in namespaces: %v",
			e.Chart, e.Details.FoundNamespaces)
	case ErrorTypeDuplicateInNamespace:
		return fmt.Sprintf("Multiple instances in namespace: %s in %s (releases: %v)",
			e.Chart, e.Details.Namespace, e.Details.FoundReleases)
	case ErrorTypeInvalidDependencyFile:
		return fmt.Sprintf("Invalid dependencies file: %s (line %d): %s",
			e.Details.File, e.Details.Line, e.Message)
	case ErrorTypeHelmClientError:
		return fmt.Sprintf("Helm client error: %s", e.Message)
	case ErrorTypeInvalidVersionConstraint:
		return fmt.Sprintf("Invalid version constraint: %s for chart %s", e.Message, e.Chart)
	default:
		return fmt.Sprintf("Unknown error: %s", e.Message)
	}
}

// NewValidationError creates a new ValidationError with the specified type and details
func NewValidationError(errorType ErrorType, chart, message string, details ErrorDetails) ValidationError {
	return ValidationError{
		Type:    errorType,
		Chart:   chart,
		Message: message,
		Details: details,
	}
}

// IsSystemNamespace checks if a namespace is a system namespace
func IsSystemNamespace(namespace string) bool {
	systemNamespaces := map[string]bool{
		"kube-system":     true,
		"kube-public":     true,
		"kube-node-lease": true,
		"default":         false, // default is not considered a system namespace
	}

	isSystem, exists := systemNamespaces[namespace]
	return exists && isSystem
}

// DependencyStatus constants
const (
	StatusSatisfied       = "satisfied"
	StatusNotFound        = "not_found"
	StatusVersionMismatch = "version_mismatch"
	StatusMultipleFound   = "multiple_found"
	StatusError           = "error"
)
