// Copyright 2026 Redpanda Data, Inc.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.md
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0

package steps

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	framework "github.com/redpanda-data/redpanda-operator/harpoon"
)

func TestPatchManifestNodePorts(t *testing.T) {
	const all = "${NODEPORT_ADMIN} ${NODEPORT_KAFKA} ${NODEPORT_HTTP} ${NODEPORT_SR}"

	// Rows run in order against one allocator: blocks are handed out on first
	// use per namespace and must stay stable for later manifests.
	steps := []struct {
		namespace string
		want      string
	}{
		{namespace: "feature-a", want: "30000 30001 30002 30003"},
		{namespace: "feature-b", want: "30004 30005 30006 30007"},
		{namespace: "feature-a", want: "30000 30001 30002 30003"},
		{namespace: "feature-c", want: "30008 30009 30010 30011"},
	}

	resetNodePortBlocks(t)
	for _, step := range steps {
		ft := &fakeT{namespace: step.namespace}
		require.Equal(t, step.want, PatchManifest(ft, all), "namespace %q", step.namespace)
		require.Empty(t, ft.fatal)
	}
}

func TestPatchManifestNodePortsExhausted(t *testing.T) {
	resetNodePortBlocks(t)

	// 30000-30079 holds exactly 20 blocks of 4.
	for i := range 20 {
		ft := &fakeT{namespace: fmt.Sprintf("feature-%d", i)}
		PatchManifest(ft, "${NODEPORT_SR}")
		require.Empty(t, ft.fatal, "block %d", i)
	}

	ft := &fakeT{namespace: "feature-20"}
	PatchManifest(ft, "${NODEPORT_ADMIN}")
	require.Contains(t, ft.fatal, "exhausted")
}

func resetNodePortBlocks(t *testing.T) {
	nodePortBlocks.Lock()
	saved := nodePortBlocks.byNamespace
	nodePortBlocks.byNamespace = map[string]int{}
	nodePortBlocks.Unlock()

	t.Cleanup(func() {
		nodePortBlocks.Lock()
		nodePortBlocks.byNamespace = saved
		nodePortBlocks.Unlock()
	})
}

// fakeT implements only what PatchManifest touches; any other call panics on
// the nil embedded interface.
type fakeT struct {
	framework.TestingT
	namespace string
	fatal     string
}

func (f *fakeT) Namespace() string { return f.namespace }

func (f *fakeT) Fatalf(format string, args ...any) { f.fatal = fmt.Sprintf(format, args...) }
