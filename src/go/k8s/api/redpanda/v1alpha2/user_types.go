package v1alpha2

import (
	"fmt"

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
	// +kubebuilder:default={conditions: {{type: "Synced", status: "Pending", reason:"Unreconciled", message:"Waiting for controller", lastTransitionTime: "1970-01-01T00:00:00Z"}}}
	Status UserStatus `json:"status,omitempty"`
}

var _ KafkaConnectedObject = (*User)(nil)
var _ AdminConnectedObject = (*User)(nil)
var _ ClusterReferencingObject = (*User)(nil)

// RedpandaName identifies the unique username created within a Redpanda cluster
// for a specific user based on its namespace and name in Kubernetes.
func (u User) RedpandaName() string {
	if u.Spec.UsernameOverride != nil {
		return *u.Spec.UsernameOverride
	}
	return fmt.Sprintf("%s/%s", u.Namespace, u.Name)
}

// ACLName constructs the name of a User for defining ACLs.
func (u User) ACLName() string {
	return "User:" + u.RedpandaName()
}

func (u *User) GetKafkaAPISpec() *KafkaAPISpec {
	return u.Spec.KafkaAPISpec
}

func (u *User) GetAdminAPISpec() *AdminAPISpec {
	return u.Spec.AdminAPISpec
}

func (u *User) GetClusterRef() *ClusterRef {
	return u.Spec.ClusterRef
}

// UserSpec defines the configuration of a Redpanda user.
type UserSpec struct {
	// ClusterRef is a reference to the cluster where the user should be created.
	// It is used in constructing the client created to configure a cluster.
	// This takes precedence over KafkaAPISpec and AdminAPISpec.
	ClusterRef *ClusterRef `json:"clusterRef,omitempty"`
	// KafkaAPISpec is the configuration information for communicating with the Kafka
	// API of a Redpanda cluster where the User should be created.
	KafkaAPISpec *KafkaAPISpec `json:"kafkaApi,omitempty"`
	// AdminAPISpec is the configuration information for communicating with the Admin
	// API of a Redpanda cluster where the User should be created.
	AdminAPISpec *AdminAPISpec `json:"adinApi,omitempty"`
	// Authentication defines the authentication information for a user. If no
	// Authentication credentials are specified, then no user will be created.
	// This is useful when wanting to manage ACLs for an already-existing user.
	Authentication *UserAuthenticationSpec `json:"authentication,omitempty"`
	// Authorization rules defined for this user.
	Authorization *UserAuthorizationSpec `json:"authorization,omitempty"`
	// Template to specify how user secrets are generated.
	Template *UserTemplateSpec `json:"template,omitempty"`
	// UsernameOverride allows for specifying a particular username for the
	// given user rather than the calculated "<NAMESPACE>/<NAME>".
	UsernameOverride *string `json:"usernameOverride,omitempty"`
}

// UserTemplateSpec defines the template metadata for a user
type UserTemplateSpec struct {
	// Specifies how the Secret with a user password is generated.
	Secret *ResourceTemplate `json:"secret,omitempty"`
}

// UserAuthenticationSpec defines the authentication mechanism enabled for this Redpanda user.
type UserAuthenticationSpec struct {
	// +kubebuilder:validation:Enum=scram-sha-256;scram-sha-512
	// +kubebuilder:validation:Required
	Type string `json:"type"`
	// Password specifies where a password is read from.
	// +kubebuilder:validation:Required
	Password Password `json:"password"`
}

type Password struct {
	// +kubebuilder:validation:Required
	ValueFrom PasswordSource `json:"valueFrom"`
}

type PasswordSource struct {
	// SecretKeyRef specifies the secret used in reading a User password.
	// If the Secret exists and has a value in it, then that value is used.
	// If the Secret does not exist, or is empty, a password is generated and
	// stored based on this configuration.
	// +kubebuilder:validation:Required
	SecretKeyRef SecretKeyRef `json:"secretKeyRef"`
}

// Authorization rules for this user.
type UserAuthorizationSpec struct {
	// +kubebuilder:validation:Enum=simple
	// +kubebuilder:default=simple
	// +kubebuilder:validation:Required
	Type string `json:"type"`
	// List of ACL rules which should be applied to this user.
	ACLs []ACLRule `json:"acls,omitempty"`
}

// Defines an ACL rule applied to the given user.
type ACLRule struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=allow;deny
	Type string `json:"type"`
	// Indicates the resource for which given ACL rule applies.
	// +kubebuilder:validation:Required
	Resource ACLResourceSpec `json:"resource"`
	// The host from which the action described in the ACL rule is allowed or denied.
	// If not set, it defaults to *, allowing or denying the action from any host.
	// +kubebuilder:default:*
	Host string `json:"host"`
	// List of operations which will be allowed or denied.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:item:Enum=Read;Write;Delete;Alter;Describe;All;IdempotentWrite;ClusterAction;Create;AlterConfigs;DescribeConfigs
	Operations []string `json:"operations"`
}

// Indicates the resource for which given ACL rule applies.
type ACLResourceSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=topic;group;cluster;transactionalId
	Type string `json:"type"`
	// Name of resource for which given ACL rule applies.
	// Can be combined with patternType field to use prefix pattern.
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// Describes the pattern used in the resource field. The supported types are literal
	// and prefix. With literal pattern type, the resource field will be used as a definition
	// of a full topic name. With prefix pattern type, the resource name will be used only as
	// a prefix. Default value is literal.
	//
	// +kubebuilder:validation:Enum=prefix;literal
	// +kubebuilder:default=literal
	PatternType string `json:"patternType"`
}

// UserStatus defines the observed state of a Redpanda user
type UserStatus struct {
	// Specifies the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration"`
	// Conditions holds the conditions for the Redpanda user.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ClusterRef is a reference to the cluster where the user was created.
	// This is used so that if a ClusterRef of a User is changed, we can
	// properly clean up the User created in the previous cluster.
	ClusterRef *ClusterRef `json:"clusterRef,omitempty"`
}

// UserList contains a list of Redpanda user objects.
// +kubebuilder:object:root=true
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Specifies a list of Redpanda user resources.
	Items []User `json:"items"`
}
