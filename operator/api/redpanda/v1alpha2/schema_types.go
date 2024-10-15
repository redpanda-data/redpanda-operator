// Copyright 2024 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package v1alpha2

import (
	"fmt"
	"hash/fnv"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/twmb/franz-go/pkg/sr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func init() {
	SchemeBuilder.Register(&Schema{}, &SchemaList{})
}

// Schema defines the CRD for a Redpanda schema.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=schemas
// +kubebuilder:resource:shortName=sc
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Latest Version",type="number",JSONPath=`.status.versions[-1]`
type Schema struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda schema.
	Spec SchemaSpec `json:"spec"`
	// Represents the current status of the Redpanda schema.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status SchemaStatus `json:"status,omitempty"`
}

var _ ClusterReferencingObject = (*Schema)(nil)

func (s *Schema) GetClusterSource() *ClusterSource {
	return s.Spec.ClusterSource
}

// SchemaType specifies the type of the given schema.
// +kubebuilder:validation:Enum=avro;protobuf
type SchemaType string

const (
	SchemaTypeAvro     SchemaType = "avro"
	SchemaTypeProtobuf SchemaType = "protobuf"
)

var (
	schemaTypesFromKafka = map[sr.SchemaType]SchemaType{
		sr.TypeAvro:     SchemaTypeAvro,
		sr.TypeProtobuf: SchemaTypeProtobuf,
	}
	schemaTypesToKafka = map[SchemaType]sr.SchemaType{
		SchemaTypeAvro:     sr.TypeAvro,
		SchemaTypeProtobuf: sr.TypeProtobuf,
	}
)

func (s SchemaType) ToKafka() sr.SchemaType {
	return schemaTypesToKafka[s]
}

func SchemaTypeFromKafka(s sr.SchemaType) SchemaType {
	return schemaTypesFromKafka[s]
}

// +kubebuilder:validation:Enum=None;Backward;BackwardTransitive;Forward;ForwardTransitive;Full;FullTransitive
type CompatibilityLevel string

const (
	CompatabilityLevelNone               CompatibilityLevel = "None"
	CompatabilityLevelBackward           CompatibilityLevel = "Backward"
	CompatabilityLevelBackwardTransitive CompatibilityLevel = "BackwardTransitive"
	CompatabilityLevelForward            CompatibilityLevel = "Forward"
	CompatabilityLevelForwardTransitive  CompatibilityLevel = "ForwardTransitive"
	CompatabilityLevelFull               CompatibilityLevel = "Full"
	CompatabilityLevelFullTransitive     CompatibilityLevel = "FullTransitive"
)

var (
	compatibilityLevelsFromKafka = map[sr.CompatibilityLevel]CompatibilityLevel{
		sr.CompatNone:               CompatabilityLevelNone,
		sr.CompatBackward:           CompatabilityLevelBackward,
		sr.CompatBackwardTransitive: CompatabilityLevelBackwardTransitive,
		sr.CompatForward:            CompatabilityLevelForward,
		sr.CompatForwardTransitive:  CompatabilityLevelForwardTransitive,
		sr.CompatFull:               CompatabilityLevelFull,
		sr.CompatFullTransitive:     CompatabilityLevelFullTransitive,
	}
	compatibilityLevelsToKafka = map[CompatibilityLevel]sr.CompatibilityLevel{
		CompatabilityLevelNone:               sr.CompatNone,
		CompatabilityLevelBackward:           sr.CompatBackward,
		CompatabilityLevelBackwardTransitive: sr.CompatBackwardTransitive,
		CompatabilityLevelForward:            sr.CompatForward,
		CompatabilityLevelForwardTransitive:  sr.CompatForwardTransitive,
		CompatabilityLevelFull:               sr.CompatFull,
		CompatabilityLevelFullTransitive:     sr.CompatFullTransitive,
	}
)

func (c CompatibilityLevel) ToKafka() sr.CompatibilityLevel {
	return compatibilityLevelsToKafka[c]
}

func CompatibilityLevelFromKafka(c sr.CompatibilityLevel) CompatibilityLevel {
	return compatibilityLevelsFromKafka[c]
}

// SchemaSpec defines the configuration of a Redpanda schema.
type SchemaSpec struct {
	// ClusterSource is a reference to the cluster hosting the schema registry.
	// It is used in constructing the client created to configure a cluster.
	// +required
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.schemaRegistry: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.schemaRegistry)`
	ClusterSource *ClusterSource `json:"cluster"`
	// Text is the actual unescaped text of a schema.
	// +required
	Text string `json:"text"`
	// Type is the type of a schema. The default type is avro.
	//
	// +kubebuilder:default=avro
	Type *SchemaType `json:"schemaType,omitempty"`

	// References declares other schemas this schema references. See the
	// docs on SchemaReference for more details.
	References []SchemaReference `json:"references,omitempty"`

	// CompatibilityLevel sets the compatibility level for the given schema
	// +kubebuilder:default=Backward
	CompatibilityLevel *CompatibilityLevel `json:"compatibilityLevel,omitempty"`
}

func (s *SchemaSpec) SchemaHash() (string, error) {
	hasher := fnv.New32()
	if _, err := hasher.Write([]byte(s.Text)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

func (s *SchemaSpec) GetCompatibilityLevel() CompatibilityLevel {
	if s.CompatibilityLevel == nil {
		return CompatabilityLevelBackward
	}
	return *s.CompatibilityLevel
}

func (s *SchemaSpec) GetType() SchemaType {
	if s.Type == nil {
		return SchemaTypeAvro
	}
	return *s.Type
}

// SchemaReference is a way for a one schema to reference another. The
// details for how referencing is done are type specific; for example,
// JSON objects that use the key "$ref" can refer to another schema via
// URL.
type SchemaReference struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version int    `json:"version"`
}

func (s *SchemaReference) ToKafka() sr.SchemaReference {
	return sr.SchemaReference{
		Name:    s.Name,
		Subject: s.Subject,
		Version: s.Version,
	}
}

func SchemaReferenceToKafka(s SchemaReference) sr.SchemaReference {
	return s.ToKafka()
}

func SchemaReferenceFromKafka(s sr.SchemaReference) SchemaReference {
	return SchemaReference{
		Name:    s.Name,
		Subject: s.Subject,
		Version: s.Version,
	}
}

// SchemaStatus defines the observed state of a Redpanda schema.
type SchemaStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda schema.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Versions shows the versions of a given schema
	Versions []int `json:"versions,omitempty"`
	// SchemaHash is the hashed value of the schema synced to the cluster
	SchemaHash string `json:"schemaHash,omitempty"`
}

// SchemaList contains a list of Redpanda schema objects.
// +kubebuilder:object:root=true
type SchemaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda schema resources.
	Items []Schema `json:"items"`
}

func (s *SchemaList) GetItems() []*Schema {
	return functional.MapFn(ptr.To, s.Items)
}
