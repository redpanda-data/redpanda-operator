package kafka

import (
	"encoding/json"

	redpandachart "github.com/redpanda-data/helm-charts/charts/redpanda"
	"github.com/redpanda-data/helm-charts/pkg/gotohelm/helmette"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/src/go/k8s/api/redpanda/v1alpha2"
)

func releaseAndPartialsFor(cluster *redpandav1alpha2.Redpanda) (release helmette.Release, partial redpandachart.PartialValues, err error) {
	var values []byte

	values, err = json.Marshal(cluster.Spec.ClusterSpec)
	if err != nil {
		return
	}

	err = json.Unmarshal(values, &partial)
	if err != nil {
		return
	}

	release = helmette.Release{
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
		Service:   "redpanda",
		IsInstall: true,
	}

	return
}
