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
		Use:   "helm-depcheck CHART_PATH",
		Short: "Check Helm chart dependencies compatibility",
		Long: `A Helm plugin that validates chart dependencies by checking
deployed releases compatibility with semver constraints.

This tool reads dependencies.yaml from your chart and validates that
deployed releases meet the specified version constraints.`,
		Version: version,
		Args:    cobra.ExactArgs(1),
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

  # Check with specific namespace pattern
  helm dependency-check --namespace-pattern "develop.*" ./charts/api

  # Output in JSON format
  helm dependency-check --output json ./frontend

  # Include system namespaces in search
  helm dependency-check --namespace-pattern ".*" ./system-chart

  # Use specific kubeconfig
  helm dependency-check --kubeconfig /path/to/config ./my-chart`

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitFailure)
	}
}

func runCheck(cmd *cobra.Command, args []string) error {
	config.ChartPath = args[0]

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
	// Check if chart path exists
	if _, err := os.Stat(config.ChartPath); os.IsNotExist(err) {
		return fmt.Errorf("chart path does not exist: %s", config.ChartPath)
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

	// Print matched namespaces
	if len(result.MatchedNamespaces) > 0 {
		fmt.Printf("Matched Namespaces (%d): %s\n",
			len(result.MatchedNamespaces),
			strings.Join(result.MatchedNamespaces, ", "))
	} else {
		fmt.Println("No namespaces matched the pattern")
	}

	if result.Summary.Total == 0 {
		fmt.Println("No dependencies found in dependencies.yaml")
		return nil
	}

	fmt.Printf("Total Dependencies: %d\n", result.Summary.Total)
	fmt.Printf("✓ Satisfied: %d\n", result.Summary.Satisfied)

	if result.Summary.NotFound > 0 {
		fmt.Printf("✗ Not Found: %d\n", result.Summary.NotFound)
	}
	if result.Summary.Mismatched > 0 {
		fmt.Printf("✗ Version Mismatch: %d\n", result.Summary.Mismatched)
	}
	if result.Summary.Multiple > 0 {
		fmt.Printf("✗ Multiple Found: %d\n", result.Summary.Multiple)
	}
	if result.Summary.Errors > 0 {
		fmt.Printf("✗ Errors: %d\n", result.Summary.Errors)
	}

	fmt.Println()

	// Print detailed results
	if config.Verbose || !result.Success {
		fmt.Println("Detailed Results:")
		fmt.Println("-----------------")

		for _, dep := range result.Dependencies {
			status := getStatusSymbol(dep.Status)
			fmt.Printf("%s %s (required: %s)", status, dep.Name, dep.RequiredVersion)

			if config.Verbose || dep.Status != types.StatusSatisfied {
				if len(dep.FoundReleases) > 0 {
					for _, release := range dep.FoundReleases {
						fmt.Printf("    Found: %s/%s (version: %s)\n",
							release.Namespace, release.Name, release.Chart.Version)
					}
				}
				if dep.Error != "" {
					fmt.Printf("    Error: %s\n", dep.Error)
				}
			}
			fmt.Println()
		}
	}

	// Print errors
	if len(result.Errors) > 0 {
		fmt.Println("Errors:")
		fmt.Println("-------")
		for _, err := range result.Errors {
			fmt.Println(err.Error())
			fmt.Println()
		}
	}

	// Print final status
	if result.Success {
		fmt.Println("✓ All dependencies satisfied!")
	} else {
		fmt.Println("✗ Dependency check failed!")
	}

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
