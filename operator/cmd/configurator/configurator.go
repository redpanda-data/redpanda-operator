// Copyright 2025 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

// Package configurator contains exports a subcommand that configures the
// redpanda.yaml for V1 clusters based on Pod metadata and environment
// variables.
package configurator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/moby/sys/mountinfo"
	"github.com/redpanda-data/redpanda/src/go/rpk/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/networking"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/resources"
	"github.com/redpanda-data/redpanda-operator/operator/pkg/utils"
	pkgsecrets "github.com/redpanda-data/redpanda-operator/pkg/secrets"
)

const (
	configDestinationEnvVar                              = "CONFIG_DESTINATION"
	configSourceDirEnvVar                                = "CONFIG_SOURCE_DIR"
	externalConnectivityAddressTypeEnvVar                = "EXTERNAL_CONNECTIVITY_ADDRESS_TYPE"
	externalConnectivityEnvVar                           = "EXTERNAL_CONNECTIVITY"
	externalConnectivityKafkaEndpointTemplateEnvVar      = "EXTERNAL_CONNECTIVITY_KAFKA_ENDPOINT_TEMPLATE"
	externalConnectivityPandaProxyEndpointTemplateEnvVar = "EXTERNAL_CONNECTIVITY_PANDA_PROXY_ENDPOINT_TEMPLATE"
	externalConnectivitySubDomainEnvVar                  = "EXTERNAL_CONNECTIVITY_SUBDOMAIN"
	hostIPEnvVar                                         = "HOST_IP_ADDRESS"
	hostNameEnvVar                                       = "HOSTNAME"
	hostPortEnvVar                                       = "HOST_PORT"
	hostIndexOffsetEnvVar                                = "HOST_INDEX_OFFSET"
	nodeNameEnvVar                                       = "NODE_NAME"
	proxyHostPortEnvVar                                  = "PROXY_HOST_PORT"
	rackAwarenessEnvVar                                  = "RACK_AWARENESS"
	validateMountedVolumeEnvVar                          = "VALIDATE_MOUNTED_VOLUME"
	redpandaRPCPortEnvVar                                = "REDPANDA_RPC_PORT"
	svcFQDNEnvVar                                        = "SERVICE_FQDN"
	additionalListenersLegacyEnvVar                      = "ADDITIONAL_LISTENERS"
	additionalListenersJSONEnvVar                        = "ADDITIONAL_LISTENERS_JSON"
	bootstrapDestinationEnvVar                           = "BOOTSTRAP_DESTINATION"
	bootstrapTemplateEnvVar                              = "BOOTSTRAP_TEMPLATE"
)

type brokerID int

type configuratorConfig struct {
	configDestination                              string
	configSourceDir                                string
	externalConnectivity                           bool
	externalConnectivityAddressType                corev1.NodeAddressType
	externalConnectivityKafkaEndpointTemplate      string
	externalConnectivityPandaProxyEndpointTemplate string
	hostIP                                         string
	hostName                                       string
	hostPort                                       int
	nodeName                                       string
	proxyHostPort                                  int
	rackAwareness                                  bool
	validateMountedVolume                          bool
	redpandaRPCPort                                int
	subdomain                                      string
	svcFQDN                                        string
	additionalListenersLegacy                      string
	additionalListenersJSON                        string
	hostIndexOffset                                int
	bootstrapDestination                           string
	bootstrapTemplate                              string
}

func (c *configuratorConfig) String() string {
	return fmt.Sprintf("The configuration:\n"+
		"hostName: %s\n"+
		"svcFQDN: %s\n"+
		"configSourceDir: %s\n"+
		"configDestination: %s\n"+
		"nodeName: %s\n"+
		"externalConnectivity: %t\n"+
		"externalConnectivitySubdomain: %s\n"+
		"externalConnectivityAddressType: %s\n"+
		"redpandaRPCPort: %d\n"+
		"hostPort: %d\n"+
		"proxyHostPort: %d\n"+
		"rackAwareness: %t\n"+
		"validateMountedVolume: %t\n"+
		"additionalListenersLegacy: %s\n"+
		"additionalListenersJSON: %s\n"+
		"hostIndexOffset: %d\n",
		c.hostName,
		c.svcFQDN,
		c.configSourceDir,
		c.configDestination,
		c.nodeName,
		c.externalConnectivity,
		c.subdomain,
		c.externalConnectivityAddressType,
		c.redpandaRPCPort,
		c.hostPort,
		c.proxyHostPort,
		c.rackAwareness,
		c.validateMountedVolume,
		c.additionalListenersLegacy,
		c.additionalListenersJSON,
		c.hostIndexOffset)
}

var errorMissingEnvironmentVariable = errors.New("missing environment variable")

func Command() *cobra.Command {
	var (
		cloudSecretsEnabled          bool
		cloudSecretsPrefix           string
		cloudSecretsAWSRegion        string
		cloudSecretsAWSRoleARN       string
		cloudSecretsGCPProjectID     string
		cloudSecretsAzureKeyVaultURI string
	)
	cmd := &cobra.Command{
		Use:     "configurator",
		Short:   "Configure redpanda.yaml based on Pod metadata",
		Aliases: []string{"configure"},
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			var cloudExpander *pkgsecrets.CloudExpander
			if cloudSecretsEnabled {
				cloudConfig := pkgsecrets.ExpanderCloudConfiguration{}
				if cloudSecretsAWSRegion != "" && cloudSecretsAWSRoleARN != "" {
					cloudConfig.AWSRegion = cloudSecretsAWSRegion
					cloudConfig.AWSRoleARN = cloudSecretsAWSRoleARN
				} else if cloudSecretsGCPProjectID != "" {
					cloudConfig.GCPProjectID = cloudSecretsGCPProjectID
				} else if cloudSecretsAzureKeyVaultURI != "" {
					cloudConfig.AzureKeyVaultURI = cloudSecretsAzureKeyVaultURI
				} else {
					log.Fatal("Cloud secrets are enabled but configuration for cloud provider is missing or invalid")
				}
				var err error
				cloudExpander, err = pkgsecrets.NewCloudExpander(ctx, cloudSecretsPrefix, cloudConfig)
				if err != nil {
					log.Fatalf("Unable to start manager: %s", err)
				}
			}

			run(
				ctx,
				cloudExpander,
			)
		},
	}

	// secret store related flags
	cmd.Flags().BoolVar(&cloudSecretsEnabled, "enable-cloud-secrets", false, "Set to true if config values can reference secrets from cloud secret store")
	cmd.Flags().StringVar(&cloudSecretsPrefix, "cloud-secrets-prefix", "", "Prefix for all names of cloud secrets")
	cmd.Flags().StringVar(&cloudSecretsAWSRegion, "cloud-secrets-aws-region", "", "AWS Region in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAWSRoleARN, "cloud-secrets-aws-role-arn", "", "AWS role ARN to assume when fetching secrets")
	cmd.Flags().StringVar(&cloudSecretsGCPProjectID, "cloud-secrets-gcp-project-id", "", "GCP project ID in which the secrets are stored")
	cmd.Flags().StringVar(&cloudSecretsAzureKeyVaultURI, "cloud-secrets-azure-key-vault-uri", "", "Azure Key Vault URI in which the secrets are stored")

	return cmd
}

func run(
	ctx context.Context,
	cloudExpander *pkgsecrets.CloudExpander,
) {
	log.Print("The redpanda configurator is starting")

	c, err := checkEnvVars()
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to get the environment variables: %w", err))
	}

	log.Print(c.String())

	p := path.Join(c.configSourceDir, "redpanda.yaml")
	cf, err := os.ReadFile(p)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to read the redpanda configuration file, %q: %w", p, err))
	}
	cfg := config.ProdDefault()
	err = yaml.Unmarshal(cf, cfg)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to parse the redpanda configuration file, %q: %w", p, err))
	}

	err = validateMountedVolume(cfg, c.validateMountedVolume)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to pass validation for the mounted volume: %w", err))
	}

	kafkaAPIPort, err := getInternalKafkaAPIPort(cfg)
	if err != nil {
		log.Fatal(err)
	}
	hostIndex, err := hostIndex(c.hostName)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to extract host index: %w", err))
	}

	log.Printf("Host index calculated %d", hostIndex)

	err = registerAdvertisedKafkaAPI(&c, cfg, hostIndex, kafkaAPIPort)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to register advertised Kafka API: %w", err))
	}

	if cfg.Pandaproxy != nil && len(cfg.Pandaproxy.PandaproxyAPI) > 0 {
		proxyAPIPort := getInternalProxyAPIPort(cfg)
		err = registerAdvertisedPandaproxyAPI(&c, cfg, hostIndex, proxyAPIPort)
		if err != nil {
			log.Fatalf("%s", fmt.Errorf("unable to register advertised Pandaproxy API: %w", err))
		}
	}

	// New bootstrap with v22.3, if redpanda.empty_seed_starts_cluster is false redpanda automatically
	// generated IDs and forms clusters using the full set of nodes.
	if cfg.Redpanda.EmptySeedStartsCluster != nil && !*cfg.Redpanda.EmptySeedStartsCluster {
		cfg.Redpanda.ID = nil
	} else {
		cfg.Redpanda.ID = new(int)
		*cfg.Redpanda.ID = int(hostIndex)

		// In case of a single seed server, the list should contain the current node itself.
		// Normally the cluster is able to recognize it's talking to itself, except when the cluster is
		// configured to use mutual TLS on the Kafka API (see Helm test).
		// So, we clear the list of seeds to help Redpanda.
		if len(cfg.Redpanda.SeedServers) == 1 {
			cfg.Redpanda.SeedServers = []config.SeedServer{}
		}
	}

	if c.rackAwareness {
		zone, zoneID, errZone := getZoneLabels(c.nodeName)
		if errZone != nil {
			log.Fatalf("%s", fmt.Errorf("unable to retrieve zone labels: %w", errZone))
		}
		populateRack(cfg, zone, zoneID)
	}

	if err = setAdditionalListeners(c.additionalListenersLegacy, c.additionalListenersJSON, c.hostIP, int(hostIndex), cfg, c.hostIndexOffset); err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to set additional listeners: %w", err))
	}

	// Perform optional fixups if required
	cfg, err = applyFixups(cfg, path.Join(c.configSourceDir, "redpanda.yaml.fixups"))
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to apply fixups to redpanda.yaml: %w", err))
	}

	cfgBytes, err := yaml.Marshal(cfg)
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to marshal the configuration: %w", err))
	}

	if err := os.WriteFile(c.configDestination, cfgBytes, 0o600); err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to write the destination configuration file: %w", err))
	}

	if c.bootstrapTemplate != "" && c.bootstrapDestination != "" {
		// Perform the bootstrap templating
		err := templateBootstrapYaml(ctx, cloudExpander, c.bootstrapTemplate, c.bootstrapDestination, path.Join(c.configSourceDir, "bootstrap.yaml.fixups"))
		if err != nil {
			log.Fatalf("%s", fmt.Errorf("unable to template the .bootstrap.yaml file: %w", err))
		}
	}

	log.Printf("Configuration saved to: %s", c.configDestination)
}

func validateMountedVolume(cfg *config.RedpandaYaml, validate bool) error {
	if !validate {
		return nil
	}
	dir, err := os.Open(cfg.Redpanda.Directory)
	if err != nil {
		return fmt.Errorf("unable to open Redpanda directory (%s): %w", cfg.Redpanda.Directory, err)
	}
	defer func() {
		if errClose := dir.Close(); errClose != nil {
			log.Printf("Error closing file: %s, %s\n", cfg.Redpanda.Directory, errClose)
		}
	}()

	stat, err := dir.Stat()
	if err != nil {
		return fmt.Errorf("unable to stat the dir: %s: %w", cfg.Redpanda.Directory, err)
	}

	if !stat.IsDir() {
		return fmt.Errorf("%s is not a directory", cfg.Redpanda.Directory) //nolint:goerr113 // Error will not be validated, but rather returned to the end user of configurator
	}

	info, err := mountinfo.GetMounts(mountinfo.FSTypeFilter("xfs"))
	if err != nil {
		return fmt.Errorf("%s must have an xfs formatted filesystem. unable to find xfs file system in /proc/self/mountinfo: %w", cfg.Redpanda.Directory, err)
	}

	if len(info) == 0 {
		return fmt.Errorf("%s must have an xfs formatted filesystem. returned mount info (/proc/self/mountinfo) does not have any xfs file system", cfg.Redpanda.Directory) //nolint:goerr113 // Error will not be validated, but rather returned to the end user of configurator
	}

	found := false
	for _, fs := range info {
		if fs.Mountpoint == cfg.Redpanda.Directory {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("returned XFS mount info list (/proc/self/mountinfo) does not have Redpanda directory (%s)", cfg.Redpanda.Directory) //nolint:goerr113 // Error will not be validated, but rather returned to the end user of configurator
	}

	file := filepath.Join(cfg.Redpanda.Directory, "testing.file")
	err = os.WriteFile(file, []byte("test-content"), 0o600)
	if err != nil {
		return fmt.Errorf("unable to write to test file (%s): %w", file, err)
	}

	err = os.Remove(file)
	if err != nil {
		return fmt.Errorf("unable to remove test file (%s): %w", file, err)
	}

	return nil
}

var errInternalPortMissing = errors.New("port configuration is missing internal port")

func getZoneLabels(nodeName string) (zone, zoneID string, err error) {
	node, err := getNode(nodeName)
	if err != nil {
		return "", "", fmt.Errorf("unable to retrieve node: %w", err)
	}
	zone = node.Labels["topology.kubernetes.io/zone"]
	zoneID = node.Labels["topology.cloud.redpanda.com/zone-id"]
	return zone, zoneID, nil
}

func populateRack(cfg *config.RedpandaYaml, zone, zoneID string) {
	cfg.Redpanda.Rack = zoneID
	if zoneID == "" {
		cfg.Redpanda.Rack = zone
	}
}

func getInternalKafkaAPIPort(cfg *config.RedpandaYaml) (int, error) {
	for _, l := range cfg.Redpanda.KafkaAPI {
		if l.Name == "kafka" {
			return l.Port, nil
		}
	}
	return 0, fmt.Errorf("%w %v", errInternalPortMissing, cfg.Redpanda.KafkaAPI)
}

func getInternalProxyAPIPort(cfg *config.RedpandaYaml) int {
	for _, l := range cfg.Pandaproxy.PandaproxyAPI {
		if l.Name == "proxy" {
			return l.Port
		}
	}
	return 0
}

func getNode(nodeName string) (*corev1.Node, error) {
	k8sconfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to create in cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(k8sconfig)
	if err != nil {
		return nil, fmt.Errorf("unable to create clientset: %w", err)
	}

	node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve node: %w", err)
	}
	return node, nil
}

func registerAdvertisedKafkaAPI(
	c *configuratorConfig, cfg *config.RedpandaYaml, index brokerID, kafkaAPIPort int,
) error {
	cfg.Redpanda.AdvertisedKafkaAPI = []config.NamedSocketAddress{
		{
			Address: c.hostName + "." + c.svcFQDN,
			Port:    kafkaAPIPort,
			Name:    resources.InternalListenerName,
		},
	}

	if !c.externalConnectivity {
		return nil
	}

	if c.subdomain != "" {
		data := utils.NewEndpointTemplateData(int(index), c.hostIP, c.hostIndexOffset)
		ep, err := utils.ComputeEndpoint(c.externalConnectivityKafkaEndpointTemplate, data)
		if err != nil {
			return err
		}

		cfg.Redpanda.AdvertisedKafkaAPI = append(cfg.Redpanda.AdvertisedKafkaAPI, config.NamedSocketAddress{
			Address: fmt.Sprintf("%s.%s", ep, c.subdomain),
			Port:    c.hostPort,
			Name:    resources.DefaultExternalKafkaListenerName,
		})
		return nil
	}

	node, err := getNode(c.nodeName)
	if err != nil {
		return fmt.Errorf("unable to retrieve node: %w", err)
	}

	cfg.Redpanda.AdvertisedKafkaAPI = append(cfg.Redpanda.AdvertisedKafkaAPI, config.NamedSocketAddress{
		Address: networking.GetPreferredAddress(node, c.externalConnectivityAddressType),
		Port:    c.hostPort,
		Name:    resources.DefaultExternalKafkaListenerName,
	})

	return nil
}

func registerAdvertisedPandaproxyAPI(
	c *configuratorConfig, cfg *config.RedpandaYaml, index brokerID, proxyAPIPort int,
) error {
	cfg.Pandaproxy.AdvertisedPandaproxyAPI = []config.NamedSocketAddress{
		{
			Address: c.hostName + "." + c.svcFQDN,
			Port:    proxyAPIPort,
			Name:    resources.InternalProxyListenerName,
		},
	}

	if c.proxyHostPort == 0 {
		return nil
	}

	// Pandaproxy uses the Kafka API subdomain.
	if c.subdomain != "" {
		data := utils.NewEndpointTemplateData(int(index), c.hostIP, c.hostIndexOffset)
		ep, err := utils.ComputeEndpoint(c.externalConnectivityPandaProxyEndpointTemplate, data)
		if err != nil {
			return err
		}

		cfg.Pandaproxy.AdvertisedPandaproxyAPI = append(cfg.Pandaproxy.AdvertisedPandaproxyAPI, config.NamedSocketAddress{
			Address: fmt.Sprintf("%s.%s", ep, c.subdomain),
			Port:    c.proxyHostPort,
			Name:    resources.DefaultExternalProxyListenerName,
		})
		return nil
	}

	node, err := getNode(c.nodeName)
	if err != nil {
		return fmt.Errorf("unable to retrieve node: %w", err)
	}

	cfg.Pandaproxy.AdvertisedPandaproxyAPI = append(cfg.Pandaproxy.AdvertisedPandaproxyAPI, config.NamedSocketAddress{
		Address: getExternalIP(node),
		Port:    c.proxyHostPort,
		Name:    resources.DefaultExternalProxyListenerName,
	})

	return nil
}

func getExternalIP(node *corev1.Node) string {
	if node == nil {
		return ""
	}
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeExternalIP {
			return address.Address
		}
	}
	return ""
}

//nolint:funlen // envs are many
func checkEnvVars() (configuratorConfig, error) {
	var result error
	var extCon string
	var rpcPort string
	var hostPort string
	var hostIndexOffset string

	c := configuratorConfig{}

	envVarList := []struct {
		value *string
		name  string
	}{
		{
			value: &c.hostName,
			name:  hostNameEnvVar,
		},
		{
			value: &c.svcFQDN,
			name:  svcFQDNEnvVar,
		},
		{
			value: &c.configSourceDir,
			name:  configSourceDirEnvVar,
		},
		{
			value: &c.configDestination,
			name:  configDestinationEnvVar,
		},
		{
			value: &c.nodeName,
			name:  nodeNameEnvVar,
		},
		{
			value: &c.subdomain,
			name:  externalConnectivitySubDomainEnvVar,
		},
		{
			value: &extCon,
			name:  externalConnectivityEnvVar,
		},
		{
			value: &rpcPort,
			name:  redpandaRPCPortEnvVar,
		},
		{
			value: &hostPort,
			name:  hostPortEnvVar,
		},
		{
			value: &c.externalConnectivityKafkaEndpointTemplate,
			name:  externalConnectivityKafkaEndpointTemplateEnvVar,
		},
		{
			value: &c.externalConnectivityPandaProxyEndpointTemplate,
			name:  externalConnectivityPandaProxyEndpointTemplateEnvVar,
		},
		{
			value: &c.hostIP,
			name:  hostIPEnvVar,
		},
		{
			value: &hostIndexOffset,
			name:  hostIndexOffsetEnvVar,
		},
	}
	for _, envVar := range envVarList {
		v, exist := os.LookupEnv(envVar.name)
		if !exist {
			result = errors.Join(result, fmt.Errorf("%s %w", envVar.name, errorMissingEnvironmentVariable))
		}
		*envVar.value = v
	}

	extCon, exist := os.LookupEnv(externalConnectivityEnvVar)
	if !exist {
		result = errors.Join(result, fmt.Errorf("%s %w", externalConnectivityEnvVar, errorMissingEnvironmentVariable))
	}

	var err error
	c.externalConnectivity, err = strconv.ParseBool(extCon)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("unable to parse bool: %w", err))
	}

	rackAwareness, exist := os.LookupEnv(rackAwarenessEnvVar)
	if !exist {
		result = errors.Join(result, fmt.Errorf("%s %w", rackAwarenessEnvVar, errorMissingEnvironmentVariable))
	}
	c.rackAwareness, err = strconv.ParseBool(rackAwareness)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("unable to parse bool: %w", err))
	}

	validateMountedVolume, exist := os.LookupEnv(validateMountedVolumeEnvVar)
	if !exist {
		result = errors.Join(result, fmt.Errorf("%s %w", validateMountedVolumeEnvVar, errorMissingEnvironmentVariable))
	}
	c.validateMountedVolume, err = strconv.ParseBool(validateMountedVolume)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("unable to parse bool: %w", err))
	}

	// Providing the address type is optional.
	addressType, exists := os.LookupEnv(externalConnectivityAddressTypeEnvVar)
	if exists {
		c.externalConnectivityAddressType = corev1.NodeAddressType(addressType)
	}

	c.redpandaRPCPort, err = strconv.Atoi(rpcPort)
	if err != nil {
		result = errors.Join(result, fmt.Errorf("unable to convert rpc port from string to int: %w", err))
	}

	if hostIndexOffset != "" {
		c.hostIndexOffset, err = strconv.Atoi(hostIndexOffset)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("unable to convert HOST_INDEX_OFFSET env var from string to int: %w", err))
		}
	}

	c.hostPort, err = strconv.Atoi(hostPort)
	if err != nil && c.externalConnectivity {
		result = errors.Join(result, fmt.Errorf("unable to convert host port from string to int: %w", err))
	}

	// Providing proxy host port is optional
	proxyHostPort, exist := os.LookupEnv(proxyHostPortEnvVar)
	if exist && proxyHostPort != "" {
		c.proxyHostPort, err = strconv.Atoi(proxyHostPort)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("unable to convert proxy host port from string to int: %w", err))
		}
	}

	c.additionalListenersLegacy, exist = os.LookupEnv(additionalListenersLegacyEnvVar)
	if exist {
		log.Printf("additional listeners configured with the legacy format: %v", c.additionalListenersLegacy)
	}

	c.additionalListenersJSON, exist = os.LookupEnv(additionalListenersJSONEnvVar)
	if exist {
		log.Printf("additional listeners configured with the JSON format: %v", c.additionalListenersJSON)
	}

	// Handling the templating of the bootstrap file is optional
	bootstrapTemplate, exist := os.LookupEnv(bootstrapTemplateEnvVar)
	if exist && bootstrapTemplate != "" {
		bootstrapDestination, exist := os.LookupEnv(bootstrapDestinationEnvVar)
		if !exist || bootstrapDestination == "" {
			result = errors.Join(result, fmt.Errorf("%s specified without corresponding %s", bootstrapTemplateEnvVar, bootstrapDestinationEnvVar))
		} else {
			c.bootstrapTemplate = bootstrapTemplate
			c.bootstrapDestination = bootstrapDestination
		}
	}

	return c, result
}

// hostIndex takes advantage of pod naming convention in Kubernetes StatfulSet
// the last number is the index of replica. This index is then propagated
// to redpanda.node_id.
func hostIndex(hostName string) (brokerID, error) {
	s := strings.Split(hostName, "-")
	last := len(s) - 1
	i, err := strconv.Atoi(s[last])
	return brokerID(i), err
}

type allListenersTemplateSpec struct {
	KafkaListeners           []config.NamedAuthNSocketAddress `json:"redpanda.kafka_api,omitempty" yaml:"redpanda.kafka_api,omitempty"`
	KafkaAdvertisedListeners []config.NamedSocketAddress      `json:"redpanda.advertised_kafka_api,omitempty" yaml:"redpanda.advertised_kafka_api,omitempty"`
	KafkaTLS                 []config.ServerTLS               `json:"redpanda.kafka_api_tls,omitempty" yaml:"redpanda.kafka_api_tls,omitempty"`
	ProxyListeners           []config.NamedAuthNSocketAddress `json:"pandaproxy.pandaproxy_api,omitempty" yaml:"pandaproxy.pandaproxy_api,omitempty"`
	ProxyAdvertisedListeners []config.NamedSocketAddress      `json:"pandaproxy.advertised_pandaproxy_api,omitempty" yaml:"pandaproxy.advertised_pandaproxy_api,omitempty"`
	ProxyTLS                 []config.ServerTLS               `json:"pandaproxy.pandaproxy_api_tls,omitempty" yaml:"pandaproxy.pandaproxy_api_tls,omitempty"`
	SchemaRegistryListeners  []config.NamedAuthNSocketAddress `json:"schema_registry.schema_registry_api,omitempty" yaml:"schema_registry.schema_registry_api,omitempty"`
	SchemaRegistryTLS        []config.ServerTLS               `json:"schema_registry.schema_registry_api_tls,omitempty" yaml:"schema_registry.schema_registry_api_tls,omitempty"`
}

// setAdditionalListeners sets the additional listeners in the input Redpanda config.
// It can take two types of configuration inputs, a structured JSON and a string in the legacy format.
// It tries to first decode the structured JSON input if set
// If not set, it will try to decode the legacy input.
// sample listeners config string:
//   - structured JSON: {"pandaproxy.advertised_pandaproxy_api":[{"name":"private-link-proxy","address:"{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 }}.redpanda.com","port":{{39282 | add .Index}}}],"pandaproxy.pandaproxy_api":[{"name":"private-link-proxy","address":"0.0.0.0","port":"port":{{39282 | add .Index}}}]}
//   - legacy format:   {"pandaproxy.advertised_pandaproxy_api":"[{'name':'private-link-proxy','address':'{{ .Index }}-f415bda0-{{ .HostIP | sha256sum | substr 0 }}.redpanda.com','port':{{39282 | add .Index}}}]","pandaproxy.pandaproxy_api":"[{'name':'private-link-proxy','address':'0.0.0.0','port':'port':{{39282 | add .Index}}}]"}
func setAdditionalListeners(additionalListenersCfgLegacy, additionalListenersCfgJSON, hostIP string, hostIndex int, cfg *config.RedpandaYaml, hostIndexOffset int) error {
	if (additionalListenersCfgLegacy == "" || additionalListenersCfgLegacy == "{}") && (additionalListenersCfgJSON == "" || additionalListenersCfgJSON == "{}") {
		return nil
	}

	structuredDecode := true
	additionalListenersCfg := additionalListenersCfgJSON
	if additionalListenersCfg == "" {
		additionalListenersCfg = additionalListenersCfgLegacy
		structuredDecode = false
	}

	additionalListenersCfg, err := utils.Compute(additionalListenersCfg, utils.NewEndpointTemplateData(hostIndex, hostIP, hostIndexOffset), false)
	if err != nil {
		return err
	}

	nodeConfig := config.ProdDefault()
	if structuredDecode {
		structuredAdditionalListeners := &allListenersTemplateSpec{}
		err = json.Unmarshal([]byte(additionalListenersCfg), &structuredAdditionalListeners)
		if err != nil {
			return err
		}
		nodeConfig.Redpanda.KafkaAPI = structuredAdditionalListeners.KafkaListeners
		nodeConfig.Redpanda.AdvertisedKafkaAPI = structuredAdditionalListeners.KafkaAdvertisedListeners
		nodeConfig.Redpanda.KafkaAPITLS = structuredAdditionalListeners.KafkaTLS
		nodeConfig.Pandaproxy.PandaproxyAPI = structuredAdditionalListeners.ProxyListeners
		nodeConfig.Pandaproxy.AdvertisedPandaproxyAPI = structuredAdditionalListeners.ProxyAdvertisedListeners
		nodeConfig.Pandaproxy.PandaproxyAPITLS = structuredAdditionalListeners.ProxyTLS
		nodeConfig.SchemaRegistry.SchemaRegistryAPI = structuredAdditionalListeners.SchemaRegistryListeners
		nodeConfig.SchemaRegistry.SchemaRegistryAPITLS = structuredAdditionalListeners.SchemaRegistryTLS
	} else {
		additionalListeners := map[string]string{}
		err = json.Unmarshal([]byte(additionalListenersCfg), &additionalListeners)
		if err != nil {
			return err
		}
		nodeConfig.Redpanda.KafkaAPI = []config.NamedAuthNSocketAddress{}
		nodeConfig.Redpanda.AdvertisedKafkaAPI = []config.NamedSocketAddress{}
		nodeConfig.Redpanda.KafkaAPITLS = []config.ServerTLS{}
		nodeConfig.Pandaproxy.PandaproxyAPI = []config.NamedAuthNSocketAddress{}
		nodeConfig.Pandaproxy.AdvertisedPandaproxyAPI = []config.NamedSocketAddress{}
		nodeConfig.Pandaproxy.PandaproxyAPITLS = []config.ServerTLS{}
		nodeConfig.SchemaRegistry.SchemaRegistryAPI = []config.NamedAuthNSocketAddress{}
		nodeConfig.SchemaRegistry.SchemaRegistryAPITLS = []config.ServerTLS{}

		for _, k := range resources.AdditionalListenerCfgNames {
			if v, found := additionalListeners[k]; found {
				err = config.Set(nodeConfig, k, v)
				if err != nil {
					return err
				}
			}
		}
	}
	// Merge additional listeners to the input config
	if len(nodeConfig.Redpanda.KafkaAPI) > 0 {
		setAuthnAdditionalListeners(resources.ExternalListenerName, &cfg.Redpanda.KafkaAPI, nodeConfig.Redpanda.KafkaAPI)
	}

	if len(nodeConfig.Redpanda.AdvertisedKafkaAPI) > 0 {
		setAdditionalAdvertisedListeners(resources.ExternalListenerName, &cfg.Redpanda.AdvertisedKafkaAPI, &cfg.Redpanda.KafkaAPITLS,
			nodeConfig.Redpanda.AdvertisedKafkaAPI, nodeConfig.Redpanda.KafkaAPITLS)
	}

	if nodeConfig.Pandaproxy == nil {
		return nil
	}

	if len(nodeConfig.Pandaproxy.PandaproxyAPI) > 0 {
		if cfg.Pandaproxy == nil {
			cfg.Pandaproxy = &config.Pandaproxy{}
		}
		setAuthnAdditionalListeners(resources.PandaproxyPortExternalName, &cfg.Pandaproxy.PandaproxyAPI, nodeConfig.Pandaproxy.PandaproxyAPI)
	}
	if len(nodeConfig.Pandaproxy.AdvertisedPandaproxyAPI) > 0 {
		if cfg.Pandaproxy == nil {
			cfg.Pandaproxy = &config.Pandaproxy{}
		}

		setAdditionalAdvertisedListeners(resources.PandaproxyPortExternalName, &cfg.Pandaproxy.AdvertisedPandaproxyAPI, &cfg.Pandaproxy.PandaproxyAPITLS,
			nodeConfig.Pandaproxy.AdvertisedPandaproxyAPI, nodeConfig.Pandaproxy.PandaproxyAPITLS)
	}
	if len(nodeConfig.SchemaRegistry.SchemaRegistryAPI) > 0 {
		setAuthnAdditionalListeners(resources.DefaultExternalSchemaRegistryListenerName, &cfg.SchemaRegistry.SchemaRegistryAPI, nodeConfig.SchemaRegistry.SchemaRegistryAPI)
		setTLSConfigForAdditionalListeners(resources.DefaultExternalSchemaRegistryListenerName, listenerNames(nodeConfig.SchemaRegistry.SchemaRegistryAPI),
			&cfg.SchemaRegistry.SchemaRegistryAPITLS, nodeConfig.SchemaRegistry.SchemaRegistryAPITLS)
	}

	return nil
}

func listenerNames(listeners []config.NamedAuthNSocketAddress) []string {
	names := make([]string, len(listeners))
	for i, l := range listeners {
		names[i] = l.Name
	}
	return names
}

func advertisedListenerNames(listeners []config.NamedSocketAddress) []string {
	names := make([]string, len(listeners))
	for i, l := range listeners {
		names[i] = l.Name
	}
	return names
}

// setAuthnAdditionalListeners populates the authentication config in the addtiional listeners with the config from the external listener,
// and append the additional listeners to the input listeners.
func setAuthnAdditionalListeners(externalListenerName string, listeners *[]config.NamedAuthNSocketAddress, additionalListeners []config.NamedAuthNSocketAddress) {
	var externalListenerCfg *config.NamedAuthNSocketAddress
	for i := 0; i < len(*listeners); i++ {
		cfg := &(*listeners)[i]
		if cfg.Name == externalListenerName {
			externalListenerCfg = cfg
			break
		}
	}
	if externalListenerCfg == nil {
		*listeners = unique(append(*listeners, additionalListeners...), func(l config.NamedAuthNSocketAddress) string { return l.Name })
		return
	}
	// Use the authn methold of the default external listener if authn method is not set in additional listener.
	for i := 0; i < len(additionalListeners); i++ {
		cfg := &additionalListeners[i]
		if cfg.AuthN == nil || *cfg.AuthN == "" {
			cfg.AuthN = externalListenerCfg.AuthN
		}
	}
	*listeners = unique(append(*listeners, additionalListeners...), func(l config.NamedAuthNSocketAddress) string { return l.Name })
}

// setAdditionalAdvertisedListeners populates the TLS config and address in the addtiional listeners with the config from the external listener,
// and append the additional listeners to the input advertised listeners and TLS configs.
func setAdditionalAdvertisedListeners(externalListenerName string, advListeners *[]config.NamedSocketAddress, tlsCfgs *[]config.ServerTLS, additionalAdvListeners []config.NamedSocketAddress, additionalTLSCfgs []config.ServerTLS) {
	var externalAPICfg *config.NamedSocketAddress
	i := slices.IndexFunc(*advListeners, func(l config.NamedSocketAddress) bool { return l.Name == externalListenerName })
	if i != -1 {
		externalAPICfg = &(*advListeners)[i]
	} else {
		i = slices.IndexFunc(*advListeners, func(l config.NamedSocketAddress) bool { return !strings.Contains(l.Address, "svc.cluster.local") })
		if i != -1 {
			externalAPICfg = &(*advListeners)[i]
		}
	}

	if externalAPICfg != nil {
		// Use the address of the default external listener if address is not set in additional listener.
		for i := 0; i < len(additionalAdvListeners); i++ {
			cfg := &additionalAdvListeners[i]
			if cfg.Address == "" {
				cfg.Address = externalAPICfg.Address
			}
		}
	}

	*advListeners = unique(append(*advListeners, additionalAdvListeners...), func(l config.NamedSocketAddress) string { return l.Name })
	setTLSConfigForAdditionalListeners(externalListenerName, advertisedListenerNames(additionalAdvListeners), tlsCfgs, additionalTLSCfgs)
}

// setTLSConfigForAdditionalListeners sets the TLS config in the additional listeners if not set.
// Each of additionalTLSCfgs might not have all the fields (CertFile, KeyFile, RequiredClientAuth, TruststoreFile) set.
// The function tries to set each of the unset fields.
// It looks for the default external listener or the first listener if not found, and
// use the TLS configuration in the listener to set each of the ServerTLS fields.
func setTLSConfigForAdditionalListeners(defaultExternalListenerName string, listenerNames []string, tlsCfgs *[]config.ServerTLS, additionalTLSCfgs []config.ServerTLS) {
	var serverTLSCfg *config.ServerTLS
	// Find TLS config for the default listener.
	for i := 0; i < len(*tlsCfgs); i++ {
		tlsCfg := &(*tlsCfgs)[i]
		if tlsCfg.Name == defaultExternalListenerName {
			serverTLSCfg = tlsCfg
			break
		}
	}
	if serverTLSCfg == nil {
		if len(*tlsCfgs) == 0 {
			return
		}
		// Use the first TLS config if the default listener TLS config is not set.
		serverTLSCfg = &(*tlsCfgs)[0]
	}

	for i := 0; i < len(listenerNames); i++ {
		m := slices.IndexFunc(additionalTLSCfgs, func(t config.ServerTLS) bool { return t.Name == listenerNames[i] })
		if m != -1 {
			cfg := additionalTLSCfgs[m]
			cfg.Enabled = true
			if cfg.CertFile == "" {
				cfg.CertFile = serverTLSCfg.CertFile
			}
			if cfg.KeyFile == "" {
				cfg.KeyFile = serverTLSCfg.KeyFile
			}
			if cfg.RequireClientAuth {
				if cfg.TruststoreFile == "" {
					cfg.TruststoreFile = serverTLSCfg.TruststoreFile
				}
			} else if cfg.TruststoreFile != "" {
				cfg.RequireClientAuth = true
			}
			if cfg.Other == nil {
				cfg.Other = serverTLSCfg.Other
			}
			*tlsCfgs = append(*tlsCfgs, cfg)
		} else {
			// additionalTLSCfgs does not have a config for the listener, use the default listener TLS config.
			*tlsCfgs = append(*tlsCfgs, config.ServerTLS{
				Name:              listenerNames[i],
				Enabled:           serverTLSCfg.Enabled,
				CertFile:          serverTLSCfg.CertFile,
				KeyFile:           serverTLSCfg.KeyFile,
				TruststoreFile:    serverTLSCfg.TruststoreFile,
				RequireClientAuth: serverTLSCfg.RequireClientAuth,
				Other:             serverTLSCfg.Other,
			})
		}
	}
	*tlsCfgs = unique(*tlsCfgs, func(t config.ServerTLS) string { return t.Name })
}

func unique[T any](slice []T, getKey func(T) string) []T {
	seen := make(map[string]bool)
	result := []T{}

	for _, val := range slice {
		key := getKey(val)
		if _, ok := seen[key]; !ok {
			seen[key] = true
			result = append(result, val)
		}
	}
	return result
}
