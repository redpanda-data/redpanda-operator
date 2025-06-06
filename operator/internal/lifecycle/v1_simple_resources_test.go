package lifecycle

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/labels"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
)

const (
	externalDNSHostname  = "external-dns.alpha.kubernetes.io/hostname"
	externalDNSUseHostIP = "external-dns.alpha.kubernetes.io/use-external-host-ip"
)

// mockConfigTemplater implements resources.ConfigTemplater for testing
type mockConfigTemplater struct{}

func (m *mockConfigTemplater) Templates() (map[string]string, error) {
	return map[string]string{
		"redpanda.yaml":       "test: content",
		".bootstrap.json.in": "{\"test\": \"bootstrap\"}",
	}, nil
}

func getNameWithSuffix(name, suffix string) string {
	const separator = "-"
	const maxNameLength = 253 - 1 // DNS1123SubdomainMaxLength minus 1 for separator
	if len(name)+len(suffix)+len(separator) > maxNameLength {
		name = name[:maxNameLength-len(suffix)-len(separator)]
	}
	return name + separator + suffix
}

func TestV1SimpleResourceRenderer_Render(t *testing.T) {
	testCases := []struct {
		name     string
		testFile string
	}{
		{
			name:     "standard_cluster",
			testFile: "testdata/v1_simple_resource_renderer_gold.yaml",
		},
		{
			name:     "byoc_cluster",
			testFile: "testdata/v1_simple_resource_renderer_byoc_gold.yaml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Load the golden file
			goldenBytes, err := os.ReadFile(tc.testFile)
			require.NoError(t, err, "failed to read golden file")

			// Parse the golden file
			goldenCluster := &vectorizedv1alpha1.Cluster{}
			err = yaml.Unmarshal(goldenBytes, goldenCluster)
			require.NoError(t, err, "failed to unmarshal golden file")

	// Create a test client
	scheme := runtime.NewScheme()
	err = vectorizedv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)
	err = rbacv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = policyv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = networkingv1.AddToScheme(scheme)
	require.NoError(t, err)

	testClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a mock config templater factory
	configFactory := func(cluster *vectorizedv1alpha1.Cluster) resources.ConfigTemplater {
		return &mockConfigTemplater{}
	}
	
	// Create the renderer
	renderer := V1SimpleResourceRenderer{
		Client:        testClient,
		Scheme:        scheme,
		ConfigFactory: configFactory,
		TLSSecretName: goldenCluster.Name + "-proxy-api-node",
		ClusterIssuer: "letsencrypt-dns-prod",
		SvcPorts: []resources.NamedServicePort{
			{Name: "admin", Port: 9644, TargetPort: 9644},
			{Name: "kafka", Port: 9092, TargetPort: 9092},
			{Name: "pandaproxy", Port: 8082, TargetPort: 8082},
		},
	}

	// Run the method under test
	ctx := context.Background()
	objects, err := renderer.Render(ctx, goldenCluster)
	require.NoError(t, err, "renderer.Render should not return an error")
	require.NotNil(t, objects, "renderer.Render should return objects")

	// Store the objects of each type for validation
	var serviceAccount *corev1.ServiceAccount
	var clusterRole *rbacv1.ClusterRole
	var clusterRoleBinding *rbacv1.ClusterRoleBinding
	var pdb *policyv1.PodDisruptionBudget
	var pandaProxySecret *corev1.Secret
	var schemaRegistrySecret *corev1.Secret
	var lifecycleSecret *corev1.Secret
	var configMap *corev1.ConfigMap
	var ingress *networkingv1.Ingress
	var loadBalancerService *corev1.Service
	var nodePortService *corev1.Service
	var headlessService *corev1.Service
	var clusterService *corev1.Service

	// Count the number of each type
	saCount := 0
	crCount := 0
	crbCount := 0
	pdbCount := 0
	pandaProxySecretCount := 0
	schemaRegistrySecretCount := 0
	lifecycleSecretCount := 0
	configMapCount := 0
	ingressCount := 0
	loadBalancerServiceCount := 0
	nodePortServiceCount := 0
	headlessServiceCount := 0
	clusterServiceCount := 0

	// Construct expected names for resources
	pandaProxySecretName := getNameWithSuffix(goldenCluster.Name, resources.PandaProxySuffix)
	schemaRegistrySecretName := getNameWithSuffix(goldenCluster.Name, resources.SchemaRegistrySuffix)
	lifecycleSecretName := getNameWithSuffix(goldenCluster.Name, "lifecycle")
	loadBalancerServiceName := goldenCluster.Name + "-lb-bootstrap"
	nodePortServiceName := goldenCluster.Name + "-external"
	headlessServiceName := goldenCluster.Name
	clusterServiceName := goldenCluster.Name + "-cluster"
	configMapName := getNameWithSuffix(goldenCluster.Name, "base")

	// Classify objects
	for _, obj := range objects {
		switch o := obj.(type) {
		case *corev1.ServiceAccount:
			serviceAccount = o
			saCount++
		case *rbacv1.ClusterRole:
			clusterRole = o
			crCount++
		case *rbacv1.ClusterRoleBinding:
			clusterRoleBinding = o
			crbCount++
		case *policyv1.PodDisruptionBudget:
			pdb = o
			pdbCount++
		case *corev1.Secret:
			if o.Name == pandaProxySecretName {
				pandaProxySecret = o
				pandaProxySecretCount++
			} else if o.Name == schemaRegistrySecretName {
				schemaRegistrySecret = o
				schemaRegistrySecretCount++
			} else if o.Name == lifecycleSecretName {
				lifecycleSecret = o
				lifecycleSecretCount++
			}
		case *corev1.ConfigMap:
			if o.Name == configMapName {
				configMap = o
				configMapCount++
			}
		case *networkingv1.Ingress:
			ingress = o
			ingressCount++
		case *corev1.Service:
			if o.Name == loadBalancerServiceName {
				loadBalancerService = o
				loadBalancerServiceCount++
			} else if o.Name == nodePortServiceName {
				nodePortService = o
				nodePortServiceCount++
			} else if o.Name == headlessServiceName {
				headlessService = o
				headlessServiceCount++
			} else if o.Name == clusterServiceName {
				clusterService = o
				clusterServiceCount++
			}
		}
	}

	// Validate counts
	assert.Equal(t, 1, saCount, "expected exactly one ServiceAccount")
	assert.Equal(t, 1, crCount, "expected exactly one ClusterRole")
	assert.Equal(t, 1, crbCount, "expected exactly one ClusterRoleBinding")
	assert.Equal(t, 1, pdbCount, "expected exactly one PodDisruptionBudget")
	assert.Equal(t, 1, configMapCount, "expected exactly one ConfigMap")
	assert.Equal(t, 1, lifecycleSecretCount, "expected exactly one Lifecycle Secret")
	assert.Equal(t, 1, headlessServiceCount, "expected exactly one Headless Service")
	assert.Equal(t, 1, clusterServiceCount, "expected exactly one Cluster Service")
	
	// In the golden file there should be at least one external listener with a bootstrap configuration
	// So we should expect a LoadBalancer service
	if goldenCluster.ExternalListener() != nil && goldenCluster.ExternalListener().External.Bootstrap != nil {
		assert.Equal(t, 1, loadBalancerServiceCount, "expected exactly one LoadBalancer Service")
	}
	
	// Validate Headless Service
	require.NotNil(t, headlessService, "expected a Headless Service")
	assert.Equal(t, headlessServiceName, headlessService.Name, "Headless Service name should match cluster.Name")
	assert.Equal(t, goldenCluster.Namespace, headlessService.Namespace, "Headless Service namespace should match cluster.Namespace")
	assert.Equal(t, corev1.ServiceTypeClusterIP, headlessService.Spec.Type, "Headless Service should have type ClusterIP")
	assert.Equal(t, corev1.ClusterIPNone, headlessService.Spec.ClusterIP, "Headless Service should have ClusterIP 'None'")
	assert.True(t, headlessService.Spec.PublishNotReadyAddresses, "Headless Service should publish not ready addresses")
	assert.NotEmpty(t, headlessService.Spec.Ports, "Headless Service should have ports defined")
	
	// Check if the headless service has the correct selector
	expectedLabels := labels.ForCluster(goldenCluster).AsAPISelector().MatchLabels
	assert.Equal(t, expectedLabels, headlessService.Spec.Selector, "Headless Service selector should match cluster labels")
	
	// Check for annotations if external listener with subdomain is configured
	if goldenCluster.FirstExternalListener() != nil && goldenCluster.FirstExternalListener().External.Subdomain != "" {
		assert.Contains(t, headlessService.Annotations, externalDNSHostname, "Headless Service should have external-dns.alpha.kubernetes.io/hostname annotation")
		assert.Contains(t, headlessService.Annotations, externalDNSUseHostIP, "Headless Service should have external-dns.alpha.kubernetes.io/use-external-host-ip annotation")
		assert.Equal(t, goldenCluster.FirstExternalListener().External.Subdomain, headlessService.Annotations[externalDNSHostname], "external-dns.alpha.kubernetes.io/hostname should match subdomain")
	}
	
	// Validate Cluster Service
	require.NotNil(t, clusterService, "expected a Cluster Service")
	assert.Equal(t, clusterServiceName, clusterService.Name, "Cluster Service name should match expected pattern")
	assert.Equal(t, goldenCluster.Namespace, clusterService.Namespace, "Cluster Service namespace should match cluster.Namespace")
	assert.Equal(t, corev1.ServiceTypeClusterIP, clusterService.Spec.Type, "Cluster Service should have type ClusterIP")
	assert.True(t, clusterService.Spec.PublishNotReadyAddresses, "Cluster Service should publish not ready addresses")
	assert.NotEmpty(t, clusterService.Spec.Ports, "Cluster Service should have ports defined")
	
	// Check if the cluster service has the correct selector
	expectedClusterLabels := labels.ForCluster(goldenCluster).AsAPISelector().MatchLabels
	assert.Equal(t, expectedClusterLabels, clusterService.Spec.Selector, "Cluster Service selector should match cluster labels")
	
	// Validate that we have the expected ports
	assert.Len(t, clusterService.Spec.Ports, 3, "Cluster Service should have 3 ports (admin, kafka, pandaproxy)")
	portNames := make([]string, len(clusterService.Spec.Ports))
	for i, port := range clusterService.Spec.Ports {
		portNames[i] = port.Name
		assert.Equal(t, corev1.ProtocolTCP, port.Protocol, "All ports should use TCP protocol")
	}
	assert.Contains(t, portNames, "admin", "Should have admin port")
	assert.Contains(t, portNames, "kafka", "Should have kafka port") 
	assert.Contains(t, portNames, "pandaproxy", "Should have pandaproxy port")
	
	// With our implementation, a NodePort Service should be created based on the cluster configuration
	// The test golden file should have the proper configuration for this
	hasExternalListeners := false
	
	// Check if the cluster has any external listeners that would result in NodePorts
	if len(goldenCluster.KafkaAPIExternalListeners()) > 0 || 
	   len(goldenCluster.PandaproxyAPIExternalListeners()) > 0 ||
	   (goldenCluster.Spec.Configuration.SchemaRegistry != nil && goldenCluster.Spec.Configuration.SchemaRegistry.External.Enabled) {
		hasExternalListeners = true
	}
	
	if hasExternalListeners {
		assert.Equal(t, 1, nodePortServiceCount, "expected exactly one NodePort Service with external listeners")
		
		// Validate the NodePort Service
		require.NotNil(t, nodePortService, "expected a NodePort Service with external listeners")
		
		// Check service metadata
		assert.Equal(t, nodePortServiceName, nodePortService.Name,
			"NodePort Service name should match expected pattern")
		assert.Equal(t, goldenCluster.Namespace, nodePortService.Namespace,
			"NodePort Service namespace should match cluster.Namespace")
		
		// Check service type and policy
		assert.Equal(t, corev1.ServiceTypeNodePort, nodePortService.Spec.Type,
			"NodePort Service should have type NodePort")
		assert.Equal(t, corev1.ServiceExternalTrafficPolicyTypeLocal, nodePortService.Spec.ExternalTrafficPolicy,
			"NodePort Service should have ExternalTrafficPolicyTypeLocal")
		assert.Nil(t, nodePortService.Spec.Selector,
			"NodePort Service selector should be nil for external connectivity")
		
		// Verify we have port definitions
		assert.NotEmpty(t, nodePortService.Spec.Ports, "NodePort Service should have ports defined")
	} else {
		assert.Equal(t, 0, nodePortServiceCount, "expected no NodePort Service when no external listeners are configured")
	}

	// Validate Service Account
	require.NotNil(t, serviceAccount, "expected a ServiceAccount")
	if goldenCluster.Spec.ServiceAccount != nil {
		assert.Equal(t, *goldenCluster.Spec.ServiceAccount, serviceAccount.Name,
			"ServiceAccount name should match spec.serviceAccount")
	}
	assert.Equal(t, goldenCluster.Namespace, serviceAccount.Namespace,
		"ServiceAccount namespace should match cluster.Namespace")

	// Validate PDB
	require.NotNil(t, pdb, "expected a PodDisruptionBudget")
	assert.Equal(t, goldenCluster.Name, pdb.Name, "PDB name should match cluster.Name")
	assert.Equal(t, goldenCluster.Namespace, pdb.Namespace, "PDB namespace should match cluster.Namespace")

	// Validate ClusterRole
	require.NotNil(t, clusterRole, "expected a ClusterRole")
	assert.Equal(t, "redpanda-init-configurator", clusterRole.Name, "ClusterRole name should match expected pattern")

	// Validate ClusterRoleBinding
	require.NotNil(t, clusterRoleBinding, "expected a ClusterRoleBinding")
	assert.Equal(t, "redpanda-init-configurator", clusterRoleBinding.Name, "ClusterRoleBinding name should match expected pattern")
	assert.Equal(t, "redpanda-init-configurator", clusterRoleBinding.RoleRef.Name, "ClusterRoleBinding roleRef name should match ClusterRole name")
	assert.Equal(t, "ClusterRole", clusterRoleBinding.RoleRef.Kind, "ClusterRoleBinding roleRef kind should be ClusterRole")

	// Validate ConfigMap
	require.NotNil(t, configMap, "expected a ConfigMap")
	assert.Equal(t, configMapName, configMap.Name, "ConfigMap name should match expected pattern")
	assert.Equal(t, goldenCluster.Namespace, configMap.Namespace, "ConfigMap namespace should match cluster.Namespace")
	
	// Validate ConfigMap data
	assert.Contains(t, configMap.Data, "redpanda.yaml", "ConfigMap should contain redpanda.yaml")
	assert.Contains(t, configMap.Data, ".bootstrap.json.in", "ConfigMap should contain bootstrap template")
	
	// Validate Ingress if present and enabled
	ppAPI := goldenCluster.FirstPandaproxyAPIExternal()
	if ppAPI != nil && ppAPI.External.Subdomain != "" && ppAPI.External.Ingress != nil && ppAPI.External.Ingress.Enabled != nil && *ppAPI.External.Ingress.Enabled {
		assert.Equal(t, 1, ingressCount, "expected exactly one Ingress")
		require.NotNil(t, ingress, "expected an Ingress resource")
		assert.Equal(t, goldenCluster.Name, ingress.Name, "Ingress name should match cluster name")
		assert.Equal(t, goldenCluster.Namespace, ingress.Namespace, "Ingress namespace should match cluster namespace")
		
		// Check Ingress configuration
		if ingress != nil {
			// Verify rules
			require.NotEmpty(t, ingress.Spec.Rules, "Ingress should have rules")
			assert.Equal(t, goldenCluster.FirstPandaproxyAPIExternal().External.Subdomain, ingress.Spec.Rules[0].Host, 
				"Ingress host should match subdomain")
			
			// Verify backend service
			require.NotNil(t, ingress.Spec.Rules[0].HTTP, "Ingress rule should have HTTP settings")
			require.NotEmpty(t, ingress.Spec.Rules[0].HTTP.Paths, "Ingress HTTP rule should have paths")
			require.NotNil(t, ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service, "Ingress path should have service backend")
			assert.Equal(t, goldenCluster.Name+"-cluster", ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name, 
				"Ingress backend service name should match cluster service")
			assert.Equal(t, "proxy-external", ingress.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name, 
				"Ingress backend port name should be proxy-external")
			
			// Verify TLS configuration if enabled
			proxyAPI := goldenCluster.FirstPandaproxyAPIExternal()
			if proxyAPI.TLS.Enabled {
				require.NotNil(t, ingress.Spec.TLS, "Ingress should have TLS configuration when TLS is enabled")
				require.NotEmpty(t, ingress.Spec.TLS, "Ingress TLS configuration should not be empty")
				// Validate TLS secret name follows the convention from WithTLS
				expectedSecretName := goldenCluster.Name + "-proxy-api-node"
				assert.Equal(t, expectedSecretName, ingress.Spec.TLS[0].SecretName, 
					"Ingress TLS secret name should follow <cluster-name>-proxy-api-node pattern")
				// Validate TLS hosts includes both direct and wildcard patterns (same as WithTLS)
				require.Len(t, ingress.Spec.TLS[0].Hosts, 2, "TLS hosts should include both direct and wildcard patterns")
				assert.Contains(t, ingress.Spec.TLS[0].Hosts, proxyAPI.External.Subdomain, 
					"TLS hosts should include the direct subdomain")
				assert.Contains(t, ingress.Spec.TLS[0].Hosts, fmt.Sprintf("*.%s", proxyAPI.External.Subdomain), 
					"TLS hosts should include wildcard pattern for subdomain")
				
				// If issuer is specified, validate annotations match the WithTLS function
				if proxyAPI.TLS.IssuerRef != nil {
					// Validate cert-manager annotation exists and has correct value
					require.Contains(t, ingress.Annotations, "cert-manager.io/cluster-issuer", 
						"Ingress should have cert-manager annotation when issuer is specified")
					assert.Equal(t, proxyAPI.TLS.IssuerRef.Name, ingress.Annotations["cert-manager.io/cluster-issuer"], 
						"cert-manager.io/cluster-issuer annotation should match the IssuerRef name")
					
					// Validate force-ssl-redirect annotation is set correctly
					require.Contains(t, ingress.Annotations, "nginx.ingress.kubernetes.io/force-ssl-redirect",
						"Ingress should have force-ssl-redirect annotation when TLS is enabled")
					assert.Equal(t, "true", ingress.Annotations["nginx.ingress.kubernetes.io/force-ssl-redirect"], 
						"force-ssl-redirect annotation should be set to 'true'")
				}
			} else if ingress.Spec.TLS != nil {
				assert.Empty(t, ingress.Spec.TLS, "Ingress should not have TLS configuration when TLS is not enabled")
			}
		}
	} else {
		assert.Equal(t, 0, ingressCount, "expected no Ingress when ingress is disabled or subdomain not configured")
	}

	// Check for PVC templates if storage is defined
	var nodePool vectorizedv1alpha1.NodePoolSpec
	if len(goldenCluster.Spec.NodePools) == 0 {
		// If no nodepools, use the cluster-level storage
		nodePool.Storage = goldenCluster.Spec.Storage
	} else {
		// Find the default nodepool or use the first one
		for _, np := range goldenCluster.Spec.NodePools {
			if np.Name == vectorizedv1alpha1.DefaultNodePoolName {
				nodePool = np
				break
			}
		}
		if nodePool.Name == "" && len(goldenCluster.Spec.NodePools) > 0 {
			nodePool = goldenCluster.Spec.NodePools[0]
		}
	}
	
	// The golden file has SASL enabled and both PandaProxy and SchemaRegistry configured
	assert.Equal(t, 1, pandaProxySecretCount, "expected exactly one PandaProxy Secret")
	assert.Equal(t, 1, schemaRegistrySecretCount, "expected exactly one SchemaRegistry Secret")

	// Validate PandaProxy Secret if present
	if pandaProxySecret != nil {
		assert.Equal(t, pandaProxySecretName, pandaProxySecret.Name,
			"PandaProxy Secret name should match expected pattern")
		assert.Equal(t, goldenCluster.Namespace, pandaProxySecret.Namespace,
			"PandaProxy Secret namespace should match cluster.Namespace")
		assert.Equal(t, corev1.SecretTypeBasicAuth, pandaProxySecret.Type,
			"PandaProxy Secret type should be basic-auth")
		assert.Contains(t, pandaProxySecret.Data, corev1.BasicAuthUsernameKey,
			"PandaProxy Secret should contain username")
		assert.Contains(t, pandaProxySecret.Data, corev1.BasicAuthPasswordKey,
			"PandaProxy Secret should contain password")
		assert.Equal(t, []byte(resources.ScramPandaproxyUsername),
			pandaProxySecret.Data[corev1.BasicAuthUsernameKey],
			"PandaProxy Secret username should be correct")
	}

	// Validate SchemaRegistry Secret if present
	if schemaRegistrySecret != nil {
		assert.Equal(t, schemaRegistrySecretName, schemaRegistrySecret.Name,
			"SchemaRegistry Secret name should match expected pattern")
		assert.Equal(t, goldenCluster.Namespace, schemaRegistrySecret.Namespace,
			"SchemaRegistry Secret namespace should match cluster.Namespace")
		assert.Equal(t, corev1.SecretTypeBasicAuth, schemaRegistrySecret.Type,
			"SchemaRegistry Secret type should be basic-auth")
		assert.Contains(t, schemaRegistrySecret.Data, corev1.BasicAuthUsernameKey,
			"SchemaRegistry Secret should contain username")
		assert.Contains(t, schemaRegistrySecret.Data, corev1.BasicAuthPasswordKey,
			"SchemaRegistry Secret should contain password")
		assert.Equal(t, []byte(resources.ScramSchemaRegistryUsername),
			schemaRegistrySecret.Data[corev1.BasicAuthUsernameKey],
			"SchemaRegistry Secret username should be correct")
	}
	
	// Validate Lifecycle Secret
	require.NotNil(t, lifecycleSecret, "expected a Lifecycle Secret")
	assert.Equal(t, lifecycleSecretName, lifecycleSecret.Name,
		"Lifecycle Secret name should match expected pattern")
	assert.Equal(t, goldenCluster.Namespace, lifecycleSecret.Namespace,
		"Lifecycle Secret namespace should match cluster.Namespace")
	
	// Validate Lifecycle Secret contents
	assert.Contains(t, lifecycleSecret.Data, "postStart.sh",
		"Lifecycle Secret should contain postStart.sh script")
	assert.Contains(t, lifecycleSecret.Data, "preStop.sh",
		"Lifecycle Secret should contain preStop.sh script")
	assert.NotEmpty(t, lifecycleSecret.Data["postStart.sh"],
		"postStart.sh script should not be empty")
	assert.NotEmpty(t, lifecycleSecret.Data["preStop.sh"],
		"preStop.sh script should not be empty")
		
	// Validate LoadBalancer Service if present
	if goldenCluster.FirstExternalListener() != nil && goldenCluster.FirstExternalListener().External.Bootstrap != nil {
		require.NotNil(t, loadBalancerService, "expected a LoadBalancer Service")
		assert.Equal(t, loadBalancerServiceName, loadBalancerService.Name,
			"LoadBalancer Service name should match expected pattern")
		assert.Equal(t, goldenCluster.Namespace, loadBalancerService.Namespace,
			"LoadBalancer Service namespace should match cluster.Namespace")
		assert.Equal(t, corev1.ServiceTypeLoadBalancer, loadBalancerService.Spec.Type,
			"LoadBalancer Service should have type LoadBalancer")
		assert.True(t, loadBalancerService.Spec.PublishNotReadyAddresses,
			"LoadBalancer Service should publish not ready addresses")
			
		// Validate Service ports match the bootstrap configuration
		assert.NotEmpty(t, loadBalancerService.Spec.Ports, "LoadBalancer Service should have ports defined")
	}
	
	// Validate NodePort Service if present
	if hasExternalListeners {
		require.NotNil(t, nodePortService, "expected a NodePort Service when StaticNodePort is configured")
		assert.Equal(t, nodePortServiceName, nodePortService.Name,
			"NodePort Service name should match expected pattern")
		assert.Equal(t, goldenCluster.Namespace, nodePortService.Namespace,
			"NodePort Service namespace should match cluster.Namespace")
		assert.Equal(t, corev1.ServiceTypeNodePort, nodePortService.Spec.Type,
			"NodePort Service should have type NodePort")
		assert.Equal(t, corev1.ServiceExternalTrafficPolicyTypeLocal, nodePortService.Spec.ExternalTrafficPolicy,
			"NodePort Service should have ExternalTrafficPolicyTypeLocal")
		assert.Nil(t, nodePortService.Spec.Selector,
			"NodePort Service selector should be nil for external connectivity")
			
		// Validate Service ports exist
		assert.NotEmpty(t, nodePortService.Spec.Ports, "NodePort Service should have ports defined")
	}
		})
	}
}

// Test case for a cluster without SASL
func TestV1SimpleResourceRenderer_RenderWithoutSASL(t *testing.T) {
	testCases := []struct {
		name     string
		testFile string
	}{
		{
			name:     "standard_cluster_no_sasl",
			testFile: "testdata/v1_simple_resource_renderer_gold.yaml",
		},
		{
			name:     "byoc_cluster_no_sasl",
			testFile: "testdata/v1_simple_resource_renderer_byoc_gold.yaml",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Load the golden file
			goldenBytes, err := os.ReadFile(tc.testFile)
			require.NoError(t, err, "failed to read golden file")

			// Parse the golden file
			goldenCluster := &vectorizedv1alpha1.Cluster{}
			err = yaml.Unmarshal(goldenBytes, goldenCluster)
			require.NoError(t, err, "failed to unmarshal golden file")

			// Modify the cluster to disable SASL
			goldenCluster.Spec.KafkaEnableAuthorization = nil
			goldenCluster.Spec.EnableSASL = false

	// Create a test client
	scheme := runtime.NewScheme()
	err = vectorizedv1alpha1.AddToScheme(scheme)
	require.NoError(t, err)
	err = corev1.AddToScheme(scheme)
	require.NoError(t, err)
	err = rbacv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = policyv1.AddToScheme(scheme)
	require.NoError(t, err)
	err = networkingv1.AddToScheme(scheme)
	require.NoError(t, err)

	testClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create a mock config templater factory
	configFactory := func(cluster *vectorizedv1alpha1.Cluster) resources.ConfigTemplater {
		return &mockConfigTemplater{}
	}
	
	// Create the renderer
	renderer := V1SimpleResourceRenderer{
		Client:        testClient,
		Scheme:        scheme,
		ConfigFactory: configFactory,
		TLSSecretName: goldenCluster.Name + "-proxy-api-node",
		ClusterIssuer: "letsencrypt-dns-prod",
		SvcPorts: []resources.NamedServicePort{
			{Name: "admin", Port: 9644, TargetPort: 9644},
			{Name: "kafka", Port: 9092, TargetPort: 9092},
			{Name: "pandaproxy", Port: 8082, TargetPort: 8082},
		},
	}

	// Run the method under test
	ctx := context.Background()
	objects, err := renderer.Render(ctx, goldenCluster)
	require.NoError(t, err, "renderer.Render should not return an error")
	require.NotNil(t, objects, "renderer.Render should return objects")

	// Count the resources for validation
	pandaProxySecretCount := 0
	schemaRegistrySecretCount := 0
	lifecycleSecretCount := 0
	configMapCount := 0
	ingressCount := 0
	statefulSetCount := 0
	loadBalancerServiceCount := 0
	nodePortServiceCount := 0
	headlessServiceCount := 0
	clusterServiceCount := 0

	// Construct expected names for resources
	pandaProxySecretName := getNameWithSuffix(goldenCluster.Name, resources.PandaProxySuffix)
	schemaRegistrySecretName := getNameWithSuffix(goldenCluster.Name, resources.SchemaRegistrySuffix)
	lifecycleSecretName := getNameWithSuffix(goldenCluster.Name, "lifecycle")
	loadBalancerServiceName := goldenCluster.Name + "-lb-bootstrap"
	nodePortServiceName := goldenCluster.Name + "-external"
	headlessServiceName := goldenCluster.Name
	clusterServiceName := goldenCluster.Name + "-cluster"
	configMapName := getNameWithSuffix(goldenCluster.Name, "base")

	for _, obj := range objects {
		if secret, ok := obj.(*corev1.Secret); ok {
			if secret.Name == pandaProxySecretName {
				pandaProxySecretCount++
			} else if secret.Name == schemaRegistrySecretName {
				schemaRegistrySecretCount++
			} else if secret.Name == lifecycleSecretName {
				lifecycleSecretCount++
			}
		}
		if cm, ok := obj.(*corev1.ConfigMap); ok {
			if cm.Name == configMapName {
				configMapCount++
			}
		}
		if _, ok := obj.(*networkingv1.Ingress); ok {
			ingressCount++
		}
		if _, ok := obj.(*appsv1.StatefulSet); ok {
			statefulSetCount++
		}
		if svc, ok := obj.(*corev1.Service); ok {
			if svc.Name == loadBalancerServiceName {
				loadBalancerServiceCount++
			} else if svc.Name == nodePortServiceName {
				nodePortServiceCount++
			} else if svc.Name == headlessServiceName {
				headlessServiceCount++
			} else if svc.Name == clusterServiceName {
				clusterServiceCount++
			}
		}
	}

	// Without SASL, no superuser secrets should be created
	assert.Equal(t, 0, pandaProxySecretCount, "expected no PandaProxy Secret when SASL is disabled")
	assert.Equal(t, 0, schemaRegistrySecretCount, "expected no SchemaRegistry Secret when SASL is disabled")
	
	// We should always have a Lifecycle Secret, even with SASL disabled
	assert.Equal(t, 1, lifecycleSecretCount, "expected exactly one Lifecycle Secret even with SASL disabled")

	// Even with SASL disabled, we should still have a ConfigMap
	assert.Equal(t, 1, configMapCount, "expected exactly one ConfigMap even with SASL disabled")

	// StatefulSet is not rendered by V1SimpleResourceRenderer (it's not a simple resource)
	assert.Equal(t, 0, statefulSetCount, "StatefulSet should not be rendered by V1SimpleResourceRenderer")
	
	// We should always have a Headless Service
	assert.Equal(t, 1, headlessServiceCount, "expected exactly one Headless Service even with SASL disabled")
	
	// We should always have a Cluster Service
	assert.Equal(t, 1, clusterServiceCount, "expected exactly one Cluster Service even with SASL disabled")
	
	// Check for LoadBalancer Service - should exist if external listeners with bootstrap are configured
	if goldenCluster.FirstExternalListener() != nil && goldenCluster.FirstExternalListener().External.Bootstrap != nil {
		assert.Equal(t, 1, loadBalancerServiceCount, "expected exactly one LoadBalancer Service even with SASL disabled")
	}

	// Check if the cluster has any external listeners that would result in NodePorts
	hasExternalListeners := false
	if len(goldenCluster.KafkaAPIExternalListeners()) > 0 ||
	   len(goldenCluster.PandaproxyAPIExternalListeners()) > 0 ||
	   (goldenCluster.Spec.Configuration.SchemaRegistry != nil && goldenCluster.Spec.Configuration.SchemaRegistry.External.Enabled) {
		hasExternalListeners = true
	}
	
	if hasExternalListeners {
		assert.Equal(t, 1, nodePortServiceCount, "expected exactly one NodePort Service with external listeners, even with SASL disabled")
	} else {
		assert.Equal(t, 0, nodePortServiceCount, "expected no NodePort Service when no external listeners are configured")
	}
	
	// Check for ingress resources
	ppAPI := goldenCluster.FirstPandaproxyAPIExternal()
	if ppAPI != nil && ppAPI.External.Subdomain != "" && ppAPI.External.Ingress != nil && ppAPI.External.Ingress.Enabled != nil && *ppAPI.External.Ingress.Enabled {
		assert.Equal(t, 1, ingressCount, "expected an Ingress resource when ingress is enabled, even with SASL disabled")
		
		// Get the Ingress resource for validation
		var ingress *networkingv1.Ingress
		for _, obj := range objects {
			if ing, ok := obj.(*networkingv1.Ingress); ok {
				ingress = ing
				break
			}
		}
		
		// Verify TLS configuration based on proxyAPI.TLS.Enabled
		proxyAPI := goldenCluster.FirstPandaproxyAPIExternal()
		if proxyAPI.TLS.Enabled {
			// Basic validation
			require.NotNil(t, ingress, "expected an Ingress resource")
			require.NotNil(t, ingress.Spec.TLS, "Ingress should have TLS configuration when TLS is enabled")
			require.NotEmpty(t, ingress.Spec.TLS, "Ingress TLS configuration should not be empty")
			
			// Verify TLS secret name pattern
			expectedSecretName := goldenCluster.Name + "-proxy-api-node"
			assert.Equal(t, expectedSecretName, ingress.Spec.TLS[0].SecretName, 
				"TLS secret name should follow <cluster-name>-proxy-api-node pattern")
			
			// Verify TLS hosts pattern
			assert.Len(t, ingress.Spec.TLS[0].Hosts, 2, "TLS hosts should have 2 entries")
			assert.Contains(t, ingress.Spec.TLS[0].Hosts, proxyAPI.External.Subdomain, 
				"TLS hosts should include direct subdomain")
			assert.Contains(t, ingress.Spec.TLS[0].Hosts, fmt.Sprintf("*.%s", proxyAPI.External.Subdomain),
				"TLS hosts should include wildcard subdomain")
			
			// Verify annotations when issuer is specified
			if proxyAPI.TLS.IssuerRef != nil && ingress.Annotations != nil {
				assert.Contains(t, ingress.Annotations, "cert-manager.io/cluster-issuer",
					"Should have cert-manager annotation with issuer")
				assert.Contains(t, ingress.Annotations, "nginx.ingress.kubernetes.io/force-ssl-redirect",
					"Should have SSL redirect annotation")
			}
		} else if ingress != nil && ingress.Spec.TLS != nil {
			assert.Empty(t, ingress.Spec.TLS, "Ingress should not have TLS configuration when TLS is not enabled")
		}
	} else {
		assert.Equal(t, 0, ingressCount, "expected no Ingress when ingress is disabled or external pandaproxy not configured")
	}
		})
	}
}