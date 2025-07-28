package checker

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"

	"helm-depcheck/pkg/helm"
	"helm-depcheck/pkg/parser"
	"helm-depcheck/pkg/types"
)

// Checker performs dependency compatibility checks
type Checker struct {
	helmClient *helm.Client
	parser     *parser.Parser
}

// NewChecker creates a new Checker instance
func NewChecker(helmClient *helm.Client, parser *parser.Parser) *Checker {
	return &Checker{
		helmClient: helmClient,
		parser:     parser,
	}
}

// Check performs the main dependency check operation
func (c *Checker) Check(config types.Config) (*types.CheckResult, error) {
	result := &types.CheckResult{
		Success:           true,
		Dependencies:      []types.DependencyResult{},
		Errors:            []types.ValidationError{},
		Summary:           types.ResultSummary{},
		MatchedNamespaces: []string{},
	}

	// Validate chart path
	if err := c.parser.ValidateChartPath(config.ChartPath); err != nil {
		result.Success = false
		result.Errors = append(result.Errors, types.NewValidationError(
			types.ErrorTypeInvalidDependencyFile,
			"",
			err.Error(),
			types.ErrorDetails{File: config.ChartPath},
		))
		return result, nil
	}

	// Parse dependencies
	deps, err := c.parser.ParseDependencies(config.ChartPath)
	if err != nil {
		result.Success = false
		if validationErr, ok := err.(types.ValidationError); ok {
			result.Errors = append(result.Errors, validationErr)
		} else {
			result.Errors = append(result.Errors, types.NewValidationError(
				types.ErrorTypeInvalidDependencyFile,
				"",
				err.Error(),
				types.ErrorDetails{},
			))
		}
		return result, nil
	}

	// Use provided namespace pattern or empty string for default behavior
	namespacePattern := config.NamespacePattern

	// Get matched namespaces for reporting
	matchedNamespaces, err := c.getMatchedNamespaces(namespacePattern)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, types.NewValidationError(
			types.ErrorTypeHelmClientError,
			"",
			fmt.Sprintf("failed to get matching namespaces: %v", err),
			types.ErrorDetails{},
		))
		return result, nil
	}
	result.MatchedNamespaces = matchedNamespaces

	// If no dependencies, return success
	if len(deps.Dependencies) == 0 {
		return result, nil
	}

	// Check each dependency
	for _, dep := range deps.Dependencies {
		depResult := c.checkSingleDependency(dep, namespacePattern)
		result.Dependencies = append(result.Dependencies, depResult)

		// Update summary
		result.Summary.Total++
		switch depResult.Status {
		case types.StatusSatisfied:
			result.Summary.Satisfied++
		case types.StatusNotFound:
			result.Summary.NotFound++
			result.Success = false
		case types.StatusVersionMismatch:
			result.Summary.Mismatched++
			result.Success = false
		case types.StatusMultipleFound:
			result.Summary.Multiple++
			result.Success = false
		case types.StatusError:
			result.Summary.Errors++
			result.Success = false
		}

		// Add validation errors for failed checks
		if depResult.Status != types.StatusSatisfied {
			validationError := c.createValidationError(depResult, namespacePattern)
			result.Errors = append(result.Errors, validationError)
		}
	}

	return result, nil
}

// checkSingleDependency checks a single dependency against deployed releases
func (c *Checker) checkSingleDependency(dep types.Dependency, namespacePattern string) types.DependencyResult {
	result := types.DependencyResult{
		Name:            dep.Name,
		RequiredVersion: dep.Version,
		Status:          types.StatusError,
		FoundReleases:   []types.Release{},
	}

	// Find releases for this chart
	releases, err := c.helmClient.FindReleasesByChartName(dep.Name, namespacePattern)
	if err != nil {
		result.Error = fmt.Sprintf("failed to find releases: %v", err)
		return result
	}

	// No releases found
	if len(releases) == 0 {
		result.Status = types.StatusNotFound
		return result
	}

	// Group releases by namespace to detect duplicates
	releasesByNamespace := make(map[string][]types.Release)
	for _, release := range releases {
		releasesByNamespace[release.Namespace] = append(releasesByNamespace[release.Namespace], release)
	}

	// Check for duplicates in same namespace
	for namespace, nsReleases := range releasesByNamespace {
		if len(nsReleases) > 1 {
			result.Status = types.StatusMultipleFound
			result.FoundReleases = nsReleases
			result.Error = fmt.Sprintf("multiple instances found in namespace %s", namespace)
			return result
		}
	}

	// Check for multiple namespaces
	if len(releasesByNamespace) > 1 {
		result.Status = types.StatusMultipleFound
		result.FoundReleases = releases
		namespaces := make([]string, 0, len(releasesByNamespace))
		for ns := range releasesByNamespace {
			namespaces = append(namespaces, ns)
		}
		result.Error = fmt.Sprintf("found in multiple namespaces: %s", strings.Join(namespaces, ", "))
		return result
	}

	// Single release found - check version compatibility
	release := releases[0]
	result.FoundReleases = []types.Release{release}

	compatible, err := c.isVersionCompatible(release.Chart.Version, dep.Version)
	if err != nil {
		result.Status = types.StatusError
		result.Error = fmt.Sprintf("version compatibility check failed: %v", err)
		return result
	}

	if compatible {
		result.Status = types.StatusSatisfied
	} else {
		result.Status = types.StatusVersionMismatch
	}

	return result
}

// isVersionCompatible checks if the found version satisfies the required constraint
func (c *Checker) isVersionCompatible(foundVersion, requiredConstraint string) (bool, error) {
	// Parse the found version
	version, err := semver.NewVersion(foundVersion)
	if err != nil {
		return false, fmt.Errorf("invalid found version '%s': %v", foundVersion, err)
	}

	// Parse the constraint
	constraint, err := semver.NewConstraint(requiredConstraint)
	if err != nil {
		return false, fmt.Errorf("invalid version constraint '%s': %v", requiredConstraint, err)
	}

	return constraint.Check(version), nil
}

// createValidationError creates appropriate validation error for failed dependency check
func (c *Checker) createValidationError(depResult types.DependencyResult, namespacePattern string) types.ValidationError {
	switch depResult.Status {
	case types.StatusNotFound:
		return types.NewValidationError(
			types.ErrorTypeDependencyNotFound,
			depResult.Name,
			"No releases found matching the dependency requirements",
			types.ErrorDetails{
				RequiredVersion: depResult.RequiredVersion,
				SearchPattern:   namespacePattern,
			},
		)

	case types.StatusVersionMismatch:
		release := depResult.FoundReleases[0]
		return types.NewValidationError(
			types.ErrorTypeVersionMismatch,
			depResult.Name,
			"Deployed version does not satisfy version constraint",
			types.ErrorDetails{
				RequiredVersion: depResult.RequiredVersion,
				FoundVersion:    release.Chart.Version,
				Namespace:       release.Namespace,
				Release:         release.Name,
			},
		)

	case types.StatusMultipleFound:
		if strings.Contains(depResult.Error, "multiple namespaces") {
			namespaces := make([]string, len(depResult.FoundReleases))
			for i, release := range depResult.FoundReleases {
				namespaces[i] = release.Namespace
			}
			return types.NewValidationError(
				types.ErrorTypeMultipleDeployments,
				depResult.Name,
				"Dependency found in multiple namespaces, cannot determine which to use",
				types.ErrorDetails{
					RequiredVersion: depResult.RequiredVersion,
					FoundNamespaces: namespaces,
				},
			)
		} else {
			// Multiple instances in same namespace
			releases := make([]string, len(depResult.FoundReleases))
			for i, release := range depResult.FoundReleases {
				releases[i] = release.Name
			}
			return types.NewValidationError(
				types.ErrorTypeDuplicateInNamespace,
				depResult.Name,
				"Multiple instances of the same chart found in single namespace",
				types.ErrorDetails{
					RequiredVersion: depResult.RequiredVersion,
					Namespace:       depResult.FoundReleases[0].Namespace,
					FoundReleases:   releases,
				},
			)
		}

	default:
		return types.NewValidationError(
			types.ErrorTypeHelmClientError,
			depResult.Name,
			depResult.Error,
			types.ErrorDetails{
				RequiredVersion: depResult.RequiredVersion,
			},
		)
	}
}

// ValidateConfig validates the checker configuration
func (c *Checker) ValidateConfig(config types.Config) error {
	if config.ChartPath == "" {
		return fmt.Errorf("chart path is required")
	}

	// Validate namespace pattern if provided
	if config.NamespacePattern != "" {
		if err := c.validateNamespacePattern(config.NamespacePattern); err != nil {
			return fmt.Errorf("invalid namespace pattern: %v", err)
		}
	}

	// Validate output format
	switch types.OutputFormat(config.OutputFormat) {
	case types.OutputFormatText, types.OutputFormatJSON, types.OutputFormatYAML, "":
		// Valid formats
	default:
		return fmt.Errorf("invalid output format '%s': must be one of text, json, yaml", config.OutputFormat)
	}

	return nil
}

// validateNamespacePattern validates that the namespace pattern is a valid regex
func (c *Checker) validateNamespacePattern(pattern string) error {
	_, err := regexp.Compile(pattern)
	return err
}

// getMatchedNamespaces returns namespaces that match the given pattern
func (c *Checker) getMatchedNamespaces(namespacePattern string) ([]string, error) {
	return c.helmClient.GetMatchingNamespaces(namespacePattern)
}

// GetSupportedVersionOperators returns a list of supported version operators
func GetSupportedVersionOperators() []string {
	return []string{
		"=", "!=", ">", "<", ">=", "<=", "~", "^",
		"X.Y.Z - A.B.C (hyphen range)",
		">=1.0.0 <2.0.0 (space-separated AND)",
	}
}
