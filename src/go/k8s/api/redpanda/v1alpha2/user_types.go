package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&User{}, &UserList{})
}

// User defines the CRD for a Redpanda user.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=users
// +kubebuilder:resource:shortName=rpu
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
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

// ACLName constructs the name of a User for defining ACLs.
func (u *User) ACLName() string {
	return "User:" + u.Name
}

func (u *User) GetClusterSource() *ClusterSource {
	return u.Spec.ClusterSource
}

// UserSpec defines the configuration of a Redpanda user.
type UserSpec struct {
	// ClusterSource is a reference to the cluster where the user should be created.
	// It is used in constructing the client created to configure a cluster.
	// This takes precedence over KafkaAPISpec and AdminAPISpec.
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

// UserTemplateSpec defines the template metadata for a user
type UserTemplateSpec struct {
	// Specifies how the Secret with a user password is generated.
	Secret *ResourceTemplate `json:"secret,omitempty"`
}

// UserAuthenticationSpec defines the authentication mechanism enabled for this Redpanda user.
type UserAuthenticationSpec struct {
	// +kubebuilder:validation:Enum=scram-sha-256;scram-sha-512;SCRAM-SHA-256;SCRAM-SHA-512
	// +kubebuilder:default=scram-sha-512
	Type *SASLMechanism `json:"type,omitempty"`
	// Password specifies where a password is read from.
	Password Password `json:"password"`
}

// Password specifies a password for the user.
// +kubebuilder:validation:XValidation:message="valueFrom must not be empty if no value supplied",rule=`self.value != "" || has(self.valueFrom)`
type Password struct {
	Value     string          `json:"value,omitempty"`
	ValueFrom *PasswordSource `json:"valueFrom"`
}

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
	ACLTypeAllow ACLType = "allow"
	ACLTypeDeny  ACLType = "deny"
)

// ACLOperation specifies the type of operation for an ACL.
// +kubebuilder:validation:item:Enum=Read;Write;Delete;Alter;Describe;IdempotentWrite;ClusterAction;Create;AlterConfigs;DescribeConfigs
type ACLOperation string

const (
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

// ACLRule defines an ACL rule applied to the given user.
//
// Validations taken from https://cwiki.apache.org/confluence/pages/viewpage.action?pageId=75978240
//
// +kubebuilder:validation:XValidation:message="supported topic operations are ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']",rule="self.resource.type == 'topic' ? self.operations.all(o, o in ['Alter', 'AlterConfigs', 'Create', 'Delete', 'Describe', 'DescribeConfigs', 'Read', 'Write']) : true"
// +kubebuilder:validation:XValidation:message="supported group operations are ['Delete', 'Describe', 'Read']",rule="self.resource.type == 'group' ? self.operations.all(o, o in ['Delete', 'Describe', 'Read']) : true"
// +kubebuilder:validation:XValidation:message="supported transactionalId operations are ['Describe', 'Write']",rule="self.resource.type == 'transactionalId' ? self.operations.all(o, o in ['Describe', 'Write']) : true"
// +kubebuilder:validation:XValidation:message="supported cluster operations are ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']",rule="self.resource.type == 'cluster' ? self.operations.all(o, o in ['Alter', 'AlterConfigs', 'ClusterAction', 'Create', 'Describe', 'DescribeConfigs', 'IdempotentWrite']) : true"
type ACLRule struct {
	Type ACLType `json:"type"`
	// Indicates the resource for which given ACL rule applies.
	Resource ACLResourceSpec `json:"resource"`
	// The host from which the action described in the ACL rule is allowed or denied.
	// If not set, it defaults to *, allowing or denying the action from any host.
	// +kubebuilder:default=*
	Host *string `json:"host,omitempty"`
	// List of operations which will be allowed or denied.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=11
	// +kubebuilder:validation:item:MaxLength=15
	// +kubebuilder:validation:item:UniqueItems=true
	Operations []ACLOperation `json:"operations"`
}

// PatternType specifies the type of pattern applied for ACL resource matching.
// +kubebuilder:validation:Enum=literal;prefixed
type PatternType string

const (
	PatternTypeLiteral  PatternType = "literal"
	PatternTypePrefixed PatternType = "prefixed"
)

// ResourceType specifies the type of resource an ACL is applied to.
// +kubebuilder:validation:Enum=topic;group;cluster;transactionalId
type ResourceType string

const (
	ResourceTypeTopic           ResourceType = "topic"
	ResourceTypeGroup           ResourceType = "group"
	ResourceTypeCluster         ResourceType = "cluster"
	ResourceTypeTransactionalID ResourceType = "transactionalId"
)

// ACLResourceSpec indicates the resource for which given ACL rule applies.
// +kubebuilder:validation:XValidation:message="prefixed pattern type only supported for ['group', 'topic', 'transactionalId']",rule="self.type in ['group', 'topic', 'transactionalId'] ? true : !has(self.patternType) || self.patternType != 'prefixed'"
// +kubebuilder:validation:XValidation:message="name must not be specified for type ['cluster']",rule=`self.type == "cluster" ? (self.name == "") : true`
// +kubebuilder:validation:XValidation:message="acl rules on non-cluster resources must specify a name",rule=`self.type == "cluster" ? true : (self.name != "")`
type ACLResourceSpec struct {
	Type ResourceType `json:"type"`
	// Name of resource for which given ACL rule applies.
	// Can be combined with patternType field to use prefix pattern.
	Name string `json:"name"`
	// Describes the pattern used in the resource field. The supported types are literal
	// and prefixed. With literal pattern type, the resource field will be used as a definition
	// of a full topic name. With prefix pattern type, the resource name will be used only as
	// a prefix. Default value is literal.
	//
	// +kubebuilder:default=literal
	PatternType *PatternType `json:"patternType,omitempty"`
}

// UserStatus defines the observed state of a Redpanda user
type UserStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions holds the conditions for the Redpanda user.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// UserList contains a list of Redpanda user objects.
// +kubebuilder:object:root=true
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda user resources.
	Items []User `json:"items"`
}
