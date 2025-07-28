package helm

import (
	"context"
	"fmt"
	"regexp"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"helm-depcheck/pkg/types"
)

// Client wraps Helm client functionality
type Client struct {
	settings   *cli.EnvSettings
	kubeClient kubernetes.Interface
}

// NewClient creates a new Helm client instance
func NewClient(kubeConfig string) (*Client, error) {
	settings := cli.New()

	if kubeConfig != "" {
		settings.KubeConfig = kubeConfig
	}

	// Create Kubernetes client
	config, err := buildKubeConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube config: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	return &Client{
		settings:   settings,
		kubeClient: kubeClient,
	}, nil
}

// GetReleases retrieves all deployed Helm releases matching the namespace pattern
func (c *Client) GetReleases(namespacePattern string) ([]types.Release, error) {
	namespaces, err := c.getMatchingNamespaces(namespacePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching namespaces: %v", err)
	}

	var allReleases []types.Release

	for _, namespace := range namespaces {
		releases, err := c.getReleasesInNamespace(namespace)
		if err != nil {
			// Log error but continue with other namespaces
			continue
		}
		allReleases = append(allReleases, releases...)
	}

	return allReleases, nil
}

// GetMatchingNamespaces returns namespaces that match the given pattern (public method)
func (c *Client) GetMatchingNamespaces(pattern string) ([]string, error) {
	return c.getMatchingNamespaces(pattern)
}

// getMatchingNamespaces returns namespaces that match the given pattern
func (c *Client) getMatchingNamespaces(pattern string) ([]string, error) {
	// Get all namespaces
	namespaceList, err := c.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %v", err)
	}

	var matchingNamespaces []string

	// Handle empty pattern - return all non-system namespaces
	if pattern == "" {
		for _, ns := range namespaceList.Items {
			namespace := ns.Name
			if !types.IsSystemNamespace(namespace) {
				matchingNamespaces = append(matchingNamespaces, namespace)
			}
		}
		return matchingNamespaces, nil
	}

	// Compile regex pattern for custom patterns
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid namespace pattern '%s': %v", pattern, err)
	}

	for _, ns := range namespaceList.Items {
		namespace := ns.Name
		if regex.MatchString(namespace) {
			matchingNamespaces = append(matchingNamespaces, namespace)
		}
	}

	return matchingNamespaces, nil
}

// getReleasesInNamespace retrieves all deployed releases in a specific namespace
func (c *Client) getReleasesInNamespace(namespace string) ([]types.Release, error) {
	actionConfig := new(action.Configuration)

	if err := actionConfig.Init(c.settings.RESTClientGetter(), namespace, "secret", func(format string, v ...interface{}) {}); err != nil {
		return nil, fmt.Errorf("failed to initialize action config for namespace %s: %v", namespace, err)
	}

	listAction := action.NewList(actionConfig)
	listAction.Deployed = true // Only get deployed releases
	listAction.AllNamespaces = false

	releases, err := listAction.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to list releases in namespace %s: %v", namespace, err)
	}

	var result []types.Release
	for _, rel := range releases {
		if rel.Info.Status == release.StatusDeployed {
			result = append(result, types.Release{
				Name:      rel.Name,
				Namespace: rel.Namespace,
				Chart: types.ChartInfo{
					Name:    rel.Chart.Metadata.Name,
					Version: rel.Chart.Metadata.Version,
				},
				Status:  rel.Info.Status.String(),
				Version: rel.Version,
				Updated: rel.Info.LastDeployed.Time,
			})
		}
	}

	return result, nil
}

// FindReleasesByChartName finds all releases for a specific chart name across namespaces
func (c *Client) FindReleasesByChartName(chartName, namespacePattern string) ([]types.Release, error) {
	allReleases, err := c.GetReleases(namespacePattern)
	if err != nil {
		return nil, err
	}

	var matchingReleases []types.Release
	for _, release := range allReleases {
		if release.Chart.Name == chartName {
			matchingReleases = append(matchingReleases, release)
		}
	}

	return matchingReleases, nil
}

// ValidateConnection validates the connection to Kubernetes and Helm
func (c *Client) ValidateConnection() error {
	// Test Kubernetes connection
	_, err := c.kubeClient.Discovery().ServerVersion()
	if err != nil {
		return fmt.Errorf("failed to connect to Kubernetes cluster: %v", err)
	}

	return nil
}

// buildKubeConfig builds Kubernetes config from kubeconfig file or in-cluster config
func buildKubeConfig(kubeConfigPath string) (*rest.Config, error) {
	if kubeConfigPath != "" {
		return clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	}

	// Try in-cluster config first
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}

	// Fall back to default kubeconfig
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}

// GetDefaultNamespacePattern returns the default namespace pattern
func GetDefaultNamespacePattern() string {
	return ""
}

// HealthCheck performs a basic health check of the Helm/Kubernetes environment
func (c *Client) HealthCheck() error {
	// Check Kubernetes connection
	if err := c.ValidateConnection(); err != nil {
		return err
	}

	// Try to list namespaces to verify permissions
	_, err := c.kubeClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("insufficient permissions to list namespaces: %v", err)
	}

	return nil
}
