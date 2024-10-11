package v1beta1_test

import (
	"fmt"

	"github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// How do I configure tiered storage?
func ExampleRedpandaCluster_TieredStorage() {
	print("Using literal values", v1beta1.RedpandaClusterSpec{
		Config: []corev1.EnvVar{
			{Name: "cloud_storage_access_key", Value: "LITERAL-KEY"},
			{Name: "cloud_storage_secret_key", Value: "LITERAL-KEY"},
		},
	})

	print("Using secret references ", v1beta1.RedpandaClusterSpec{
		Config: []corev1.EnvVar{
			{Name: "cloud_storage_access_key", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{},
			}},
		},
	})

	print("Using workload identity (IAM Roles)", v1beta1.RedpandaClusterSpec{
		Config: []corev1.EnvVar{
			{Name: "cloud_storage_credentials_source", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{},
			}},
		},
		NodePools: []v1beta1.NodePoolTemplate{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: v1beta1.NodePoolSpec{
					Template: v1beta1.BrokerSpec{
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Annotations: map[string]string{
									"todo": "find the right values",
								},
							},
						},
					},
				},
			},
		},
	})

	// Output: foo
}

func ExampleRedpandaCluster_Listeners() {
	print("heterogeneous", v1beta1.RedpandaClusterSpec{
		NodePools: []v1beta1.NodePoolTemplate{
			{
				Spec: v1beta1.NodePoolSpec{
					Template: v1beta1.BrokerSpec{
						Config: []corev1.EnvVar{
							{Name: ""},
						},
					},
				},
			},
		},
	})
}

func print(header string, v any) {
	out, _ := yaml.Marshal(v)
	fmt.Printf("%s\n%s\n", header, out)
}
