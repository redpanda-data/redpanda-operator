// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package multicluster

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/redpanda-data/common-go/kube"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
)

// FetchSASLUsers attempts to locate an existing SASL users secret in the cluster.
func (r *RenderState) FetchSASLUsers() (username, password, mechanism string, err error) {
	if r.client == nil {
		return
	}

	saslUsers, saslErr := secretSASLUsers(r)
	if saslErr != nil {
		err = saslErr
		return
	}
	saslUsersError := func(err error) error {
		return fmt.Errorf("error fetching SASL authentication for %s/%s: %w", saslUsers.Namespace, saslUsers.Name, err)
	}

	if saslUsers != nil {
		var users corev1.Secret
		lookupErr := r.client.Get(context.TODO(), kube.ObjectKey{Name: saslUsers.Name, Namespace: saslUsers.Namespace}, &users)
		if lookupErr != nil {
			if k8sapierrors.IsNotFound(lookupErr) {
				err = saslUsersError(errSASLSecretNotFound)
				return
			}
			err = saslUsersError(lookupErr)
			return
		}

		data, found := users.Data["users.txt"]
		if !found {
			err = saslUsersError(errSASLSecretKeyNotFound)
			return
		}

		username, password, mechanism = firstUser(data)
		if username == "" {
			err = saslUsersError(errSASLSecretSuperuserNotFound)
			return
		}
	}

	return
}

func firstUser(data []byte) (user string, password string, mechanism string) {
	file := string(data)

	for _, line := range strings.Split(file, "\n") {
		tokens := strings.Split(line, ":")

		switch len(tokens) {
		case 2:
			return tokens[0], tokens[1], "SCRAM-SHA-256"

		case 3:
			if !slices.Contains(supportedSASLMechanisms, tokens[2]) {
				continue
			}
			return tokens[0], tokens[1], tokens[2]

		default:
			continue
		}
	}

	return
}
