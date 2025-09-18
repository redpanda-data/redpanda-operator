package console

import (
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAppVersion asserts that the AppVersion const is inline with the version
// of console that's used for generating PartialConfig. In practice, it's
// acceptable for there to be a bit of difference as the config is fairly
// stable but that assertion is much harder to write.
func TestAppVersion(t *testing.T) {
	const (
		gitCmd = "git ls-remote https://github.com/redpanda-data/console.git %s | cut -c 1-12"
		goCmd  = "go list -m -json github.com/redpanda-data/console/backend | jq -r .Version | cut -d - -f 3"
	)

	gitOut, err := exec.Command("sh", "-c", fmt.Sprintf(gitCmd, AppVersion)).CombinedOutput()
	require.NoError(t, err)

	goOut, err := exec.Command("sh", "-c", goCmd).CombinedOutput()
	require.NoError(t, err)

	require.Equal(t, string(gitOut), string(goOut), ".AppVersion and go.mod should refer to the same version of console:\nAppVersion: %s\ngo.mod: %sgit: %s", AppVersion, goOut, gitOut)
}
