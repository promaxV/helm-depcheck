package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"helm-depcheck/pkg/checker"
	"helm-depcheck/pkg/helm"
	"helm-depcheck/pkg/parser"
	"helm-depcheck/pkg/types"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

var (
	config  types.Config
	version = "1.0.0"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "helm-depcheck CHART_PATH [CHART_PATH...]",
		Short: "Check Helm chart dependencies compatibility",
		Long: `A Helm plugin that validates chart dependencies by checking
deployed releases compatibility with semver constraints.

This tool reads dependencies.yaml from your charts and validates that
deployed releases meet the specified version constraints.

Supports wildcards for finding multiple charts:
  helm-depcheck ./charts/*
  helm-depcheck ./microservices/*/charts ./system-charts/*`,
		Version: version,
		Args:    cobra.MinimumNArgs(1),
		RunE:    runCheck,
	}

	// Add flags
	rootCmd.Flags().StringVarP(&config.NamespacePattern, "namespace-pattern", "p", "",
		"Regular expression for filtering namespaces (default: all non-system namespaces)")
	rootCmd.Flags().BoolVarP(&config.Verbose, "verbose", "v", false,
		"Enable verbose output")
	rootCmd.Flags().StringVarP(&config.OutputFormat, "output", "o", "text",
		"Output format (text, json, yaml)")
	rootCmd.Flags().StringVar(&config.KubeConfig, "kubeconfig", "",
		"Path to kubeconfig file")

	// Add examples
	rootCmd.Example = `  # Check dependencies for chart in current directory
  helm dependency-check ./my-chart

  # Check multiple charts with wildcards
  helm dependency-check ./charts/*

  # Check multiple specific charts
  helm dependency-check ./frontend ./backend ./database

  # Check with specific namespace pattern
  helm dependency-check --namespace-pattern "develop.*" ./charts/*

  # Output in JSON format
  helm dependency-check --output json ./charts/*

  # Mix of patterns and specific paths
  helm dependency-check ./system-charts/* ./microservices/api ./microservices/web`

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitFailure)
	}
}

func runCheck(cmd *cobra.Command, args []string) error {
	config.ChartPaths = args

	// Validate configuration
	if err := validateConfig(); err != nil {
		return fmt.Errorf("configuration validation failed: %v", err)
	}

	// Create Helm client
	helmClient, err := helm.NewClient(config.KubeConfig)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %v", err)
	}

	// Validate Helm/Kubernetes connection
	if err := helmClient.HealthCheck(); err != nil {
		return fmt.Errorf("health check failed: %v", err)
	}

	// Create parser
	parserInstance := parser.NewParser()

	// Create checker
	checkerInstance := checker.NewChecker(helmClient, parserInstance)

	// Validate checker config
	if err := checkerInstance.ValidateConfig(config); err != nil {
		return fmt.Errorf("checker configuration validation failed: %v", err)
	}

	// Perform the check
	result, err := checkerInstance.Check(config)
	if err != nil {
		return fmt.Errorf("dependency check failed: %v", err)
	}

	// Output results
	if err := outputResults(result); err != nil {
		return fmt.Errorf("failed to output results: %v", err)
	}

	// Exit with appropriate code
	if !result.Success {
		os.Exit(exitFailure)
	}

	return nil
}

func validateConfig() error {
	// Validate that we have at least one chart path
	if len(config.ChartPaths) == 0 {
		return fmt.Errorf("at least one chart path is required")
	}

	return nil
}

func outputResults(result *types.CheckResult) error {
	switch types.OutputFormat(config.OutputFormat) {
	case types.OutputFormatJSON:
		return outputJSON(result)
	case types.OutputFormatYAML:
		return outputYAML(result)
	default:
		return outputText(result)
	}
}

func outputJSON(result *types.CheckResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func outputYAML(result *types.CheckResult) error {
	encoder := yaml.NewEncoder(os.Stdout)
	defer encoder.Close()
	return encoder.Encode(result)
}

func outputText(result *types.CheckResult) error {
	// Print summary
	fmt.Println("Dependency Check Results")
	fmt.Print("========================\n\n")

	fmt.Printf("Charts Checked: %d\n", len(result.ChartResults))

	// Print matched namespaces
	if config.Verbose {
		if len(result.MatchedNamespaces) > 0 {
			fmt.Printf("Matched Namespaces (%d): %s\n",
				len(result.MatchedNamespaces),
				strings.Join(result.MatchedNamespaces, ", "))
		} else {
			fmt.Println("No namespaces matched the pattern")
		}
	}

	if result.TotalSummary.Total == 0 {
		fmt.Println("No dependencies found in any charts")
		return nil
	}

	fmt.Printf("Total Dependencies: %d\n", result.TotalSummary.Total)
	fmt.Printf("✓ Satisfied: %d\n", result.TotalSummary.Satisfied)

	if result.TotalSummary.NotFound > 0 {
		fmt.Printf("✗ Not Found: %d\n", result.TotalSummary.NotFound)
	}
	if result.TotalSummary.Mismatched > 0 {
		fmt.Printf("✗ Version Mismatch: %d\n", result.TotalSummary.Mismatched)
	}
	if result.TotalSummary.Multiple > 0 {
		fmt.Printf("✗ Multiple Found: %d\n", result.TotalSummary.Multiple)
	}
	if result.TotalSummary.Errors > 0 {
		fmt.Printf("✗ Errors: %d\n", result.TotalSummary.Errors)
	}

	fmt.Println()

	// Print results for each chart
	for _, chartResult := range result.ChartResults {
		fmt.Printf("Chart: %s (%s)\n", chartResult.ChartName, chartResult.ChartPath)
		fmt.Println(strings.Repeat("-", len(chartResult.ChartName)+len(chartResult.ChartPath)+10))

		if chartResult.Summary.Total == 0 {
			fmt.Println("  No dependencies found")
			fmt.Println()
			continue
		}

		fmt.Printf("  Dependencies: %d", chartResult.Summary.Total)
		if chartResult.Success {
			fmt.Printf(" (✓ All satisfied)")
		} else {
			fmt.Printf(" (✗ %d issues)", 
				chartResult.Summary.NotFound+chartResult.Summary.Mismatched+
				chartResult.Summary.Multiple+chartResult.Summary.Errors)
		}
		fmt.Println()

		// Print detailed results if verbose or has issues
		if config.Verbose || !chartResult.Success {
			for _, dep := range chartResult.Dependencies {
				status := getStatusSymbol(dep.Status)
				fmt.Printf("    %s %s (required: %s)", status, dep.Name, dep.RequiredVersion)

				if config.Verbose || dep.Status != types.StatusSatisfied {
					if len(dep.FoundReleases) > 0 {
						for _, release := range dep.FoundReleases {
							fmt.Println()
							fmt.Printf("        Found: %s/%s (version: %s)",
								release.Namespace, release.Name, release.Chart.Version)
						}
					}
					if dep.Error != "" {
						fmt.Printf("\n        Error: %s", dep.Error)
					}
				}
				fmt.Println()
			}
		}
		fmt.Println()
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Println("Errors:")
		fmt.Println("-------")
		for _, err := range result.Errors {
			fmt.Println(err.Error())
		}
		fmt.Println()
	}

	// Print final status
	if result.Success {
		fmt.Println("✓ All chart dependencies satisfied!")
	} else {
		fmt.Println("✗ Dependency check failed!")
	}
	fmt.Println()

	return nil
}

func getStatusSymbol(status string) string {
	switch status {
	case types.StatusSatisfied:
		return "✓"
	case types.StatusNotFound:
		return "✗"
	case types.StatusVersionMismatch:
		return "✗"
	case types.StatusMultipleFound:
		return "✗"
	case types.StatusError:
		return "✗"
	default:
		return "?"
	}
}
