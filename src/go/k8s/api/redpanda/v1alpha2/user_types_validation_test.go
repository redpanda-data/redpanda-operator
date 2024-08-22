package v1alpha2

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redpanda-data/redpanda-operator/src/go/k8s/internal/testutils"
)

func TestValidateUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*2)
	defer cancel()

	testEnv := testutils.RedpandaTestEnv{}
	cfg, err := testEnv.StartRedpandaTestEnv(false)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	baseUser := User{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: UserSpec{
			ClusterRef: &ClusterRef{
				Name: "cluster",
			},
		},
	}

	password := Password{
		ValueFrom: &PasswordSource{
			SecretKeyRef: &SecretKeyRef{
				Name: "key",
			},
		},
	}

	err = AddToScheme(scheme.Scheme)
	require.NoError(t, err)

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)
	require.NotNil(t, c)

	testCases := []struct {
		desc       string
		mutate     func(user *User)
		wantErrors []string
	}{
		{
			desc: "basic create",
		},
		// connection params
		{
			desc: "clusterRef or kafkaApiSpec and adminApiSpec - none",
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
			},
			wantErrors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		{
			desc: "clusterRef or kafkaApiSpec and adminApiSpec - admin api spec",
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.AdminAPISpec = &AdminAPISpec{
					URLs: []string{"http://1.2.3.4:0"},
				}
			},
			wantErrors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		{
			desc: "clusterRef or kafkaApiSpec and adminApiSpec - kafka api spec",
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.KafkaAPISpec = &KafkaAPISpec{
					Brokers: []string{"1.2.3.4:0"},
				}
			},
			wantErrors: []string{"either clusterRef or kafkaApiSpec and adminApiSpec must be set"},
		},
		{
			desc: "clusterRef or kafkaApiSpec and adminApiSpec - kafka api spec",
			mutate: func(user *User) {
				user.Spec.ClusterRef = nil
				user.Spec.KafkaAPISpec = &KafkaAPISpec{
					Brokers: []string{"1.2.3.4:0"},
				}
				user.Spec.AdminAPISpec = &AdminAPISpec{
					URLs: []string{"http://1.2.3.4:0"},
				}
			},
		},
		// authentication
		{
			desc: "authentication type - default",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Password: password,
				}
			},
		},
		{
			desc: "authentication type - sha-512",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type:     ptr.To("scram-sha-512"),
					Password: password,
				}
			},
		},
		{
			desc: "authentication type - sha-256",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type:     ptr.To("scram-sha-256"),
					Password: password,
				}
			},
		},
		{
			desc: "authentication type - invalid",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Type: ptr.To("invalid"),
				}
			},
			wantErrors: []string{`spec.authentication.type: Unsupported value: "invalid": supported values: "scram-sha-256", "scram-sha-512"`},
		},
		{
			desc: "authentication - no password value from",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{}
			},
			wantErrors: []string{`spec.authentication.password.valueFrom: Required value`},
		},
		{
			desc: "authentication - no secret key ref",
			mutate: func(user *User) {
				user.Spec.Authentication = &UserAuthenticationSpec{
					Password: Password{
						ValueFrom: &PasswordSource{},
					},
				}
			},
			wantErrors: []string{`spec.authentication.password.valueFrom.secretKeyRef: Required value`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			user := baseUser.DeepCopy()
			user.Name = fmt.Sprintf("name-%v", time.Now().UnixNano())

			if tc.mutate != nil {
				tc.mutate(user)
			}
			err := c.Create(ctx, user)

			if (len(tc.wantErrors) != 0) != (err != nil) {
				t.Fatalf("Unexpected response while creating User; got err=\n%v\n;want error=%v", err, tc.wantErrors != nil)
			}

			var missingErrorStrings []string
			for _, wantError := range tc.wantErrors {
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(wantError)) {
					missingErrorStrings = append(missingErrorStrings, wantError)
				}
			}
			if len(missingErrorStrings) != 0 {
				t.Errorf("Unexpected response while creating User; got err=\n%v\n;missing strings within error=%q", err, missingErrorStrings)
			}
		})
	}
}
