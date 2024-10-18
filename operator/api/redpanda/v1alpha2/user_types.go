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
	"context"
	"errors"
	"slices"

	"github.com/redpanda-data/redpanda-operator/operator/pkg/functional"
	"github.com/twmb/franz-go/pkg/kmsg"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	SchemeBuilder.Register(&User{}, &UserList{})
}

// User defines the CRD for a Redpanda user.
// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=users
// +kubebuilder:resource:shortName=rpu
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Managing User",type="boolean",JSONPath=`.status.managedUser`
// +kubebuilder:printcolumn:name="Managing ACLs",type="boolean",JSONPath=`.status.managedAcls`
// +kubebuilder:storageversion
type User struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Defines the desired state of the Redpanda user.
	Spec UserSpec `json:"spec"`
	// Represents the current status of the Redpanda user.
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Unknown", reason:"Pending", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status UserStatus `json:"status,omitempty"`
}

var _ ClusterReferencingObject = (*User)(nil)

// GetPrincipal constructs the principal of a User for defining ACLs.
func (u *User) GetPrincipal() string {
	return "User:" + u.Name
}

func (u *User) GetACLs() []ACLRule {
	if u.Spec.Authorization == nil {
		return nil
	}

	return u.Spec.Authorization.ACLs
}

func (u *User) GetClusterSource() *ClusterSource {
	return u.Spec.ClusterSource
}

func (u *User) ShouldManageUser() bool {
	return u.Spec.Authentication != nil
}

func (u *User) HasManagedUser() bool {
	return u.Status.ManagedUser
}

func (u *User) ShouldManageACLs() bool {
	return u.Spec.Authorization != nil
}

func (u *User) HasManagedACLs() bool {
	return u.Status.ManagedACLs
}

// UserSpec defines the configuration of a Redpanda user.
type UserSpec struct {
	// ClusterSource is a reference to the cluster where the user should be created.
	// It is used in constructing the client created to configure a cluster.
	// +kubebuilder:validation:XValidation:message="spec.cluster.staticConfiguration.admin: required value",rule=`!has(self.staticConfiguration) || has(self.staticConfiguration.admin)`
	// +required
	ClusterSource *ClusterSource `json:"cluster"`
	// Authentication defines the authentication information for a user. If no
	// Authentication credentials are specified, then no user will be created.
	// This is useful when wanting to manage ACLs for an already-existing user.
	Authentication *UserAuthenticationSpec `json:"authentication,omitempty"`
	// Authorization rules defined for this user.
	Authorization *UserAuthorizationSpec `json:"authorization,omitempty"`
	// Template to specify how user secrets are generated.
	Template *UserTemplateSpec `json:"template,omitempty"`
}

// UserTemplateSpec defines the template metadata (labels and annotations)
// for any subresources, such as Secrets, created by a User object.
type UserTemplateSpec struct {
	// Specifies how the Secret with a user password is generated.
	Secret *ResourceTemplate `json:"secret,omitempty"`
}

// UserAuthenticationSpec defines the authentication mechanism enabled for this Redpanda user.
type UserAuthenticationSpec struct {
	// SASL mechanism to use for the user credentials. Valid values are:
	// - scram-sha-512
	// - scram-sha-256
	// +kubebuilder:validation:Enum=scram-sha-256;scram-sha-512;SCRAM-SHA-256;SCRAM-SHA-512
	// +kubebuilder:default=scram-sha-512
	Type *SASLMechanism `json:"type,omitempty"`
	// Password specifies where a password is read from.
	Password Password `json:"password"`
}

// Password specifies a password for the user.
// +kubebuilder:validation:XValidation:message="valueFrom must not be empty if no value supplied",rule=`self.value != "" || has(self.valueFrom)`
type Password struct {
	// Value is a hardcoded value to use for the given password. It should only be used for testing purposes while
	// in production ValueFrom is preferred.
	Value string `json:"value,omitempty"`
	// ValueFrom specifies a source for a password to be fetched from when specifying or generating user credentials.
	ValueFrom *PasswordSource `json:"valueFrom"`
}

// Fetch fetches the actual value of a password based on its configuration.
func (p *Password) Fetch(ctx context.Context, c client.Client, namespace string) (string, error) {
	if p.Value != "" {
		return p.Value, nil
	}

	name := p.ValueFrom.SecretKeyRef.LocalObjectReference.Name

	var secret corev1.Secret
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if err != nil {
		return "", err
	}

	key := p.ValueFrom.SecretKeyRef.Key
	if key == "" {
		key = "password"
	}

	password, ok := secret.Data[key]
	if !ok {
		return "", errors.New("password not found in secret")
	}
	return string(password), nil
}

// PasswordSource contains the source for a password.
type PasswordSource struct {
	// SecretKeyRef specifies the secret used in reading a User password.
	// If the Secret exists and has a value in it, then that value is used.
	// If the Secret does not exist, or is empty, a password is generated and
	// stored based on this configuration.
	// +required
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef"`
}

// AuthorizationType specifies the type of authorization to use in creating a user.
// +kubebuilder:validation:Enum=simple
type AuthorizationType string

const (
	AuthorizationTypeSimple AuthorizationType = "simple"
)

// UserAuthorizationSpec defines authorization rules for this user.
type UserAuthorizationSpec struct {
	// Type specifies the type of authorization to use for User ACLs. If unspecified, defaults to `simple`. Valid values are:
	// - simple
	// +kubebuilder:default=simple
	Type *AuthorizationType `json:"type,omitempty"`
	// List of ACL rules which should be applied to this user.
	// +kubebuilder:validation:MaxItems=1024
	ACLs []ACLRule `json:"acls,omitempty"`
}

// ACLType specifies the type, either allow or deny of an ACL rule.
// +kubebuilder:validation:Enum=allow;deny
type ACLType string

const (
	ACLTypeUnknown ACLType = ""
	ACLTypeAllow   ACLType = "allow"
	ACLTypeDeny    ACLType = "deny"
)

var (
	aclTypeFromKafka = map[kmsg.ACLPermissionType]ACLType{
		kmsg.ACLPermissionTypeAllow: ACLTypeAllow,
		kmsg.ACLPermissionTypeDeny:  ACLTypeDeny,
	}
	aclTypeToKafka = map[ACLType]kmsg.ACLPermissionType{
		ACLTypeAllow: kmsg.ACLPermissionTypeAllow,
		ACLTypeDeny:  kmsg.ACLPermissionTypeDeny,
	}
)

func ACLTypeFromKafka(t kmsg.ACLPermissionType) ACLType {
	if aclType, exists := aclTypeFromKafka[t]; exists {
		return aclType
	}

	return ACLTypeUnknown
}

func (t ACLType) ToKafka() kmsg.ACLPermissionType {
	if aclType, exists := aclTypeToKafka[t]; exists {
		return aclType
	}

	return kmsg.ACLPermissionTypeUnknown
}

// ACLOperation specifies the type of operation for an ACL.
// +kubebuilder:validation:item:Enum=Read;Write;Delete;Alter;Describe;IdempotentWrite;ClusterAction;Create;AlterConfigs;DescribeConfigs
type ACLOperation string

const (
	ACLOperationUnknown         ACLOperation = ""
	ACLOperationRead            ACLOperation = "Read"
	ACLOperationWrite           ACLOperation = "Write"
	ACLOperationDelete          ACLOperation = "Delete"
	ACLOperationAlter           ACLOperation = "Alter"
	ACLOperationDescribe        ACLOperation = "Describe"
	ACLOperationIdempotentWrite ACLOperation = "IdempotentWrite"
	ACLOperationClusterAction   ACLOperation = "ClusterAction"
	ACLOperationCreate          ACLOperation = "Create"
	ACLOperationAlterConfigs    ACLOperation = "AlterConfigs"
	ACLOperationDescribeConfigs ACLOperation = "DescribeConfigs"
)

var (
	aclOperationsFromKafka = map[kmsg.ACLOperation]ACLOperation{
		kmsg.ACLOperationRead:            ACLOperationRead,
		kmsg.ACLOperationWrite:           ACLOperationWrite,
		kmsg.ACLOperationDelete:          ACLOperationDelete,
		kmsg.ACLOperationAlter:           ACLOperationAlter,
		kmsg.ACLOperationDescribe:        ACLOperationDescribe,
		kmsg.ACLOperationIdempotentWrite: ACLOperationIdempotentWrite,
		kmsg.ACLOperationClusterAction:   ACLOperationClusterAction,
		kmsg.ACLOperationCreate:          ACLOperationCreate,
		kmsg.ACLOperationAlterConfigs:    ACLOperationAlterConfigs,
		kmsg.ACLOperationDescribeConfigs: ACLOperationDescribeConfigs,
	}
	aclOperationsToKafka = map[ACLOperation]kmsg.ACLOperation{
		ACLOperationRead:            kmsg.ACLOperationRead,
		ACLOperationWrite:           kmsg.ACLOperationWrite,
		ACLOperationDelete:          kmsg.ACLOperationDelete,
		ACLOperationAlter:           kmsg.ACLOperationAlter,
		ACLOperationDescribe:        kmsg.ACLOperationDescribe,
		ACLOperationIdempotentWrite: kmsg.ACLOperationIdempotentWrite,
		ACLOperationClusterAction:   kmsg.ACLOperationClusterAction,
		ACLOperationCreate:          kmsg.ACLOperationCreate,
		ACLOperationAlterConfigs:    kmsg.ACLOperationAlterConfigs,
		ACLOperationDescribeConfigs: kmsg.ACLOperationDescribeConfigs,
	}
)

func ACLOperationFromKafka(op kmsg.ACLOperation) ACLOperation {
	if operation, exists := aclOperationsFromKafka[op]; exists {
		return operation
	}
	return ACLOperationUnknown
}

func (a ACLOperation) ToKafka() kmsg.ACLOperation {
	if operation, exists := aclOperationsToKafka[a]; exists {
		return operation
	}
	return kmsg.ACLOperationUnknown
}

// ACLRule defines an ACL rule applied to the given user.
//
// Validations taken from https://cwiki.apache.org/confluence/pages/viewpage.action?pageId=75978240
//
// +kubebuilder:validation:XValidation:message="supported topic operations are ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']",rule="self.resource.type == 'topic' ? self.operations.all(o, o in ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']) : true"
// +kubebuilder:validation:XValidation:message="supported group operations are ['Delete', 'Describe', 'Read']",rule="self.resource.type == 'group' ? self.operations.all(o, o in ['Delete', 'Describe', 'Read']) : true"
// +kubebuilder:validation:XValidation:message="supported transactionalId operations are ['Describe', 'Write']",rule="self.resource.type == 'transactionalId' ? self.operations.all(o, o in ['Describe', 'Write']) : true"
// +kubebuilder:validation:XValidation:message="supported cluster operations are ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']",rule="self.resource.type == 'cluster' ? self.operations.all(o, o in ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']) : true"
type ACLRule struct {
	// Type specifies the type of ACL rule to create. Valid values are:
	// - allow
	// - deny
	Type ACLType `json:"type"`
	// Indicates the resource for which given ACL rule applies.
	Resource ACLResourceSpec `json:"resource"`
	// The host from which the action described in the ACL rule is allowed or denied.
	// If not set, it defaults to *, allowing or denying the action from any host.
	// +kubebuilder:default=*
	Host *string `json:"host,omitempty"`
	// List of operations which will be allowed or denied. Valid values are resource type dependent, but include:
	// - Read
	// - Write
	// - Delete
	// - Alter
	// - Describe
	// - IdempotentWrite
	// - ClusterAction
	// - Create
	// - AlterConfigs
	// - DescribeConfigs
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=11
	// +kubebuilder:validation:item:MaxLength=15
	// +kubebuilder:validation:item:UniqueItems=true
	Operations []ACLOperation `json:"operations"`
}

func (r *ACLRule) GetHost() string {
	return ptr.Deref(r.Host, "*")
}

func (r *ACLRule) Equals(other ACLRule) bool { //nolint:gocritic // pass by value here is fine
	if r.Type != other.Type {
		return false
	}

	if !r.Resource.Equals(other.Resource) {
		return false
	}

	if r.GetHost() != other.GetHost() {
		return false
	}

	if len(r.Operations) != len(other.Operations) {
		return false
	}

	for _, operation := range r.Operations {
		if !slices.Contains(other.Operations, operation) {
			return false
		}
	}

	for _, operation := range other.Operations {
		if !slices.Contains(r.Operations, operation) {
			return false
		}
	}

	return true
}

// PatternType specifies the type of pattern applied for ACL resource matching.
// +kubebuilder:validation:Enum=literal;prefixed
type PatternType string

const (
	PatternTypeUnknown  PatternType = ""
	PatternTypeLiteral  PatternType = "literal"
	PatternTypePrefixed PatternType = "prefixed"
)

var (
	patternTypeFromKafka = map[kmsg.ACLResourcePatternType]PatternType{
		kmsg.ACLResourcePatternTypeLiteral:  PatternTypeLiteral,
		kmsg.ACLResourcePatternTypePrefixed: PatternTypePrefixed,
	}
	patternTypeToKafka = map[PatternType]kmsg.ACLResourcePatternType{
		PatternTypeLiteral:  kmsg.ACLResourcePatternTypeLiteral,
		PatternTypePrefixed: kmsg.ACLResourcePatternTypePrefixed,
	}
)

func ACLPatternTypeFromKafka(p kmsg.ACLResourcePatternType) PatternType {
	if patternType, exists := patternTypeFromKafka[p]; exists {
		return patternType
	}

	return PatternTypeUnknown
}

func (p *PatternType) ToKafka() kmsg.ACLResourcePatternType {
	if p == nil {
		// Literal is what we default to
		return kmsg.ACLResourcePatternTypeLiteral
	}

	if patternType, exists := patternTypeToKafka[*p]; exists {
		return patternType
	}

	return kmsg.ACLResourcePatternTypeUnknown
}

// ResourceType specifies the type of resource an ACL is applied to.
// +kubebuilder:validation:Enum=topic;group;cluster;transactionalId
type ResourceType string

const (
	ResourceTypeUnknown         ResourceType = ""
	ResourceTypeTopic           ResourceType = "topic"
	ResourceTypeGroup           ResourceType = "group"
	ResourceTypeCluster         ResourceType = "cluster"
	ResourceTypeTransactionalID ResourceType = "transactionalId"
)

var (
	resourceTypeFromKafka = map[kmsg.ACLResourceType]ResourceType{
		kmsg.ACLResourceTypeTopic:           ResourceTypeTopic,
		kmsg.ACLResourceTypeGroup:           ResourceTypeGroup,
		kmsg.ACLResourceTypeCluster:         ResourceTypeCluster,
		kmsg.ACLResourceTypeTransactionalId: ResourceTypeTransactionalID,
	}
	resourceTypeToKafka = map[ResourceType]kmsg.ACLResourceType{
		ResourceTypeTopic:           kmsg.ACLResourceTypeTopic,
		ResourceTypeGroup:           kmsg.ACLResourceTypeGroup,
		ResourceTypeCluster:         kmsg.ACLResourceTypeCluster,
		ResourceTypeTransactionalID: kmsg.ACLResourceTypeTransactionalId,
	}
)

func ResourceTypeFromKafka(t kmsg.ACLResourceType) ResourceType {
	if resourceType, exists := resourceTypeFromKafka[t]; exists {
		return resourceType
	}

	return ResourceTypeUnknown
}

func (t ResourceType) ToKafka() kmsg.ACLResourceType {
	if resourceType, exists := resourceTypeToKafka[t]; exists {
		return resourceType
	}

	return kmsg.ACLResourceTypeUnknown
}

// ACLResourceSpec indicates the resource for which given ACL rule applies.
// +kubebuilder:validation:XValidation:message="prefixed pattern type only supported for ['group', 'topic', 'transactionalId']",rule="self.type in ['group', 'topic', 'transactionalId'] ? true : !has(self.patternType) || self.patternType != 'prefixed'"
// +kubebuilder:validation:XValidation:message="name must not be specified for type ['cluster']",rule=`self.type == "cluster" ? (self.name == "") : true`
// +kubebuilder:validation:XValidation:message="acl rules on non-cluster resources must specify a name",rule=`self.type == "cluster" ? true : (self.name != "")`
type ACLResourceSpec struct {
	// Type specifies the type of resource an ACL is applied to. Valid values:
	// - topic
	// - group
	// - cluster
	// - transactionalId
	Type ResourceType `json:"type"`
	// Name of resource for which given ACL rule applies. If using type `cluster` this must not be specified.
	// Can be combined with patternType field to use prefix pattern.
	Name string `json:"name"`
	// Describes the pattern used in the resource field. The supported types are literal
	// and prefixed. With literal pattern type, the resource field will be used as a definition
	// of a full topic name. With prefix pattern type, the resource name will be used only as
	// a prefix. Prefixed patterns can only be specified when using types `topic`, `group`, or
	// `transactionalId`. Default value is literal. Valid values:
	// - literal
	// - prefixed
	//
	// +kubebuilder:default=literal
	PatternType *PatternType `json:"patternType,omitempty"`
}

func (s ACLResourceSpec) Equals(other ACLResourceSpec) bool {
	if s.Type != other.Type {
		return false
	}

	if s.GetName() != other.GetName() {
		return false
	}

	return s.GetPatternType() == other.GetPatternType()
}

func (s ACLResourceSpec) GetPatternType() PatternType {
	return ptr.Deref(s.PatternType, PatternTypeLiteral)
}

func (s ACLResourceSpec) GetName() string {
	if s.Type == ResourceTypeCluster {
		// return the singleton name
		return "kafka-cluster"
	}

	return s.Name
}

// UserStatus defines the observed state of a Redpanda user
type UserStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda user.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ManagedACLs returns whether the user has managed ACLs that need
	// to be cleaned up.
	ManagedACLs bool `json:"managedAcls,omitempty"`
	// ManagedUser returns whether the user has a managed SCRAM user that need
	// to be cleaned up.
	ManagedUser bool `json:"managedUser,omitempty"`
}

// UserList contains a list of Redpanda user objects.
// +kubebuilder:object:root=true
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda user resources.
	Items []User `json:"items"`
}

func (u *UserList) GetItems() []*User {
	return functional.MapFn(ptr.To, u.Items)
}
