package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"

	"helm-depcheck/pkg/types"
)

// Parser handles parsing and validation of dependencies.yaml files
type Parser struct{}

// NewParser creates a new Parser instance
func NewParser() *Parser {
	return &Parser{}
}

// ParseDependencies reads and parses dependencies.yaml from the given chart path
func (p *Parser) ParseDependencies(chartPath string) (*types.DependenciesFile, error) {
	dependenciesPath := filepath.Join(chartPath, "dependencies.yaml")

	// Check if dependencies.yaml exists
	data, err := os.ReadFile(dependenciesPath)
	if err != nil {
		// If file doesn't exist, return empty dependencies (no error)
		if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "cannot find") {
			return &types.DependenciesFile{Dependencies: []types.Dependency{}}, nil
		}
		return nil, types.NewValidationError(
			types.ErrorTypeInvalidDependencyFile,
			"",
			fmt.Sprintf("failed to read dependencies file: %v", err),
			types.ErrorDetails{File: dependenciesPath},
		)
	}

	var deps types.DependenciesFile
	if err := yaml.Unmarshal(data, &deps); err != nil {
		line := extractLineFromYAMLError(err)
		return nil, types.NewValidationError(
			types.ErrorTypeInvalidDependencyFile,
			"",
			fmt.Sprintf("failed to parse YAML: %v", err),
			types.ErrorDetails{
				File: dependenciesPath,
				Line: line,
			},
		)
	}

	// Validate the parsed dependencies
	if err := p.validateDependencies(&deps, dependenciesPath); err != nil {
		return nil, err
	}

	return &deps, nil
}

// validateDependencies validates the structure and content of dependencies
func (p *Parser) validateDependencies(deps *types.DependenciesFile, filePath string) error {
	seenNames := make(map[string]bool)

	for i, dep := range deps.Dependencies {
		lineNumber := i + 2 // Approximate line number (accounting for YAML structure)

		// Validate required fields
		if strings.TrimSpace(dep.Name) == "" {
			return types.NewValidationError(
				types.ErrorTypeInvalidDependencyFile,
				"",
				"dependency name cannot be empty",
				types.ErrorDetails{
					File: filePath,
					Line: lineNumber,
				},
			)
		}

		if strings.TrimSpace(dep.Version) == "" {
			return types.NewValidationError(
				types.ErrorTypeInvalidDependencyFile,
				dep.Name,
				"dependency version cannot be empty",
				types.ErrorDetails{
					File: filePath,
					Line: lineNumber,
				},
			)
		}

		// Check for duplicate names
		if seenNames[dep.Name] {
			return types.NewValidationError(
				types.ErrorTypeInvalidDependencyFile,
				dep.Name,
				"duplicate dependency name",
				types.ErrorDetails{
					File: filePath,
					Line: lineNumber,
				},
			)
		}
		seenNames[dep.Name] = true

		// Validate version constraint
		if err := p.validateVersionConstraint(dep.Version); err != nil {
			return types.NewValidationError(
				types.ErrorTypeInvalidVersionConstraint,
				dep.Name,
				err.Error(),
				types.ErrorDetails{
					File: filePath,
					Line: lineNumber,
				},
			)
		}
	}

	return nil
}

// validateVersionConstraint validates that a version constraint is valid semver
func (p *Parser) validateVersionConstraint(constraint string) error {
	// Handle empty constraint
	if strings.TrimSpace(constraint) == "" {
		return fmt.Errorf("version constraint cannot be empty")
	}

	// Try to parse as semver constraint
	_, err := semver.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid version constraint '%s': %v", constraint, err)
	}

	return nil
}

// extractLineFromYAMLError attempts to extract line number from YAML parsing errors
func extractLineFromYAMLError(err error) int {
	errStr := err.Error()

	// Try to extract line number from common YAML error patterns
	if strings.Contains(errStr, "line ") {
		var line int
		if n, parseErr := fmt.Sscanf(errStr, "%*s line %d", &line); parseErr == nil && n == 1 {
			return line
		}
	}

	return 0 // Unknown line
}

// ValidateChartPath validates that the given path contains a valid Helm chart
func (p *Parser) ValidateChartPath(chartPath string) error {
	chartYamlPath := filepath.Join(chartPath, "Chart.yaml")

	if _, err := os.ReadFile(chartYamlPath); err != nil {
		return fmt.Errorf("invalid chart path '%s': Chart.yaml not found or not readable", chartPath)
	}

	return nil
}

// GetChartInfo reads basic chart information from Chart.yaml
func (p *Parser) GetChartInfo(chartPath string) (*types.ChartInfo, error) {
	chartYamlPath := filepath.Join(chartPath, "Chart.yaml")

	data, err := os.ReadFile(chartYamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Chart.yaml: %v", err)
	}

	var chart struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	}

	if err := yaml.Unmarshal(data, &chart); err != nil {
		return nil, fmt.Errorf("failed to parse Chart.yaml: %v", err)
	}

	if chart.Name == "" {
		return nil, fmt.Errorf("chart name is required in Chart.yaml")
	}

	if chart.Version == "" {
		return nil, fmt.Errorf("chart version is required in Chart.yaml")
	}

	return &types.ChartInfo{
		Name:    chart.Name,
		Version: chart.Version,
	}, nil
}
