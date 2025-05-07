package v1beta1_test

import (
	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var Azure = v1beta1.RedpandaSpec{
	Enterprise: v1beta1.Enterprise{
		License: &v1beta1.License{
			ValueFrom: &v1beta1.LicenseValueFrom{
				SecretKeyRef: corev1.SecretKeySelector{
					// elided for brevity
				},
			},
		},
	},
	ClusterConfig: map[string]v1beta1.ValueSource{
		// No more dedicated "rack" field.
		"rack_awareness": v1beta1.ValueSource{Value: "true"},

		"aggregate_metrics": v1beta1.ValueSource{Value: "true"},
	},
	Listeners: v1beta1.Listeners{},
	NodePoolSpec: v1beta1.NodePoolSpec{
		Replicas: ptr.To[int32](5),
		BrokerTemplate: v1beta1.BrokerTemplate{
			Image:     "docker.io/redpandadata/redpanda:v24.2.12",
			Resources: corev1.ResourceRequirements{
				// elided for brevity
			},
			ValidateFSXFS:       true,
			SetDataDirOwnership: true,
			NodeConfig: map[string]v1beta1.ValueSource{
				"rack": v1beta1.ValueSource{
					Expr: v1beta1.Expr(`node_annotation("topology.kubernetes.io/zone")`),
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				// No more dedicated volume configs. Just naming conventions
				// and direct access to set the templates like on a
				// StatefulSet.
				{
					ObjectMeta: v1.ObjectMeta{
						Name: "datadir",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To("local-path"),
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("4000Gi"),
							},
						},
					},
				},
			},
		},
	},
}

// Don't do this at home.
// Assumes two or more Kubernetes clusters with a peered network and chained coreDNS instances.
// Each Kubernetes cluster would have a mild variant of this CR applied to it.
// Personal recommendation is to configure each cluster with coreDNS rules such that:
// - *.*.svc.cluster.local -> resolves to current cluster
// - *.*.svc.cluster.<cloud>-<region> -> resolves to cluster in <region> of <cloud>
var StretchCluster = v1beta1.RedpandaSpec{
	Listeners: v1beta1.Listeners{},
	NodePoolSpec: v1beta1.NodePoolSpec{
		Replicas: ptr.To[int32](5),
		BrokerTemplate: v1beta1.BrokerTemplate{
			Image:     "docker.io/redpandadata/redpanda:v24.2.12",
			Resources: corev1.ResourceRequirements{
				// elided for brevity
			},
			ValidateFSXFS:       true,
			SetDataDirOwnership: true,
			NodeConfig: map[string]v1beta1.ValueSource{
				// SRV addrs might not be the best option here but there's plenty of other options.
				"seed_servers": v1beta1.ValueSource{
					Expr: v1beta1.Expr(`
						srv_addresses("tcp", "kafka", this.headless_svc) +
						srv_addresses("tcp", "kafka", "other-cluster.namespace.svc.cluster.aws-us-east-2") +
						srv_addresses("tcp", "kafka", "other-cluster.namespace.svc.cluster.aws-us-central-1")
					`),
				},
			},
		},
	},
}
