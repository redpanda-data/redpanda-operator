// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func testTopicValidation(t *testing.T, c client.Client, ns string) {
	ctx := t.Context()

	baseTopic := Topic{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: ns,
		},
		Spec: TopicSpec{
			ClusterSource: &ClusterSource{
				ClusterRef: &ClusterRef{
					Name: "cluster",
				},
			},
		},
	}

	for name, tt := range map[string]validationTestCase[*Topic]{
		"basic create": {},
		// connection params
		"clusterRef or kafkaApiSpec - no cluster source": {
			mutate: func(topic *Topic) {
				topic.Spec.ClusterSource = nil
			},
			errors: []string{`cluster must be specified if kafkaApiSpec is not`},
		},
		"clusterRef or kafkaApiSpec - none": {
			mutate: func(topic *Topic) {
				topic.Spec.ClusterSource.ClusterRef = nil
			},
			errors: []string{`either clusterref or staticconfiguration must be set`},
		},
		"clusterRef or kafkaApiSpec - admin api spec": {
			mutate: func(topic *Topic) {
				topic.Spec.ClusterSource.ClusterRef = nil
				topic.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{}
			},
			errors: []string{`spec.cluster.staticconfiguration.kafka: required value`},
		},
		"clusterRef or kafkaApiSpec - kafka spec": {
			mutate: func(topic *Topic) {
				topic.Spec.ClusterSource.ClusterRef = nil
				topic.Spec.ClusterSource.StaticConfiguration = &StaticConfigurationSource{
					Kafka: &KafkaAPISpec{
						Brokers: []string{"1.2.3.4:0"},
					},
				}
			},
		},
		"deprecated kafkaApiSpec": {
			mutate: func(topic *Topic) {
				topic.Spec.ClusterSource = nil
				topic.Spec.KafkaAPISpec = &KafkaAPISpec{
					Brokers: []string{"1.2.3.4:0"},
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			runValidationTest(ctx, t, tt, c, &baseTopic)
		})
	}
}
