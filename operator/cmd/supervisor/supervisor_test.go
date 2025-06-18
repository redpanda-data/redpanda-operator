package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// script is a bash script that logs our received signals and exits upon
// receiving 3 in total.
const script = `
#!/usr/bin/env bash

signal_count=0

signal_handler() {
    signal_count=$((signal_count + 1))
    echo "received: $1"

    if [[ $signal_count -eq 3 || "$1" == "SIGTERM" ]]; then
        echo "exiting"
        exit 0
    fi
}

# Trap common signals
trap 'signal_handler SIGTERM' TERM
trap 'signal_handler SIGINT' INT
trap 'signal_handler SIGUSR1' USR1
trap 'signal_handler SIGUSR2' USR2
trap 'signal_handler SIGHUP' HUP

# Log AFTER traps are established.
echo "starting"

# Keep script running
while true; do
    sleep 0.1
done
`

type channelWriter chan<- string

func (w channelWriter) Write(p []byte) (n int, err error) {
	for _, chunk := range bytes.Split(p, []byte{'\n'}) {
		if len(chunk) > 0 {
			w <- string(chunk)
		}
	}
	return len(p), nil
}

func TestRunner(t *testing.T) {
	run := func(ctx context.Context, signals chan os.Signal) <-chan string {
		stdout := make(chan string)

		go func() {
			r := Runner{
				Command:     []string{"bash", "-c", script},
				GracePeriod: 5 * time.Second,
				Stdout:      channelWriter(stdout),
				Signals:     signals,
			}

			cont := r.Run(ctx)

			stdout <- fmt.Sprintf("%v", cont)

			close(stdout)
		}()

		return stdout
	}

	t.Run("signal forwarding", func(t *testing.T) {
		ctx := log.IntoContext(context.Background(), testr.New(t))
		signals := make(chan os.Signal)

		stdout := run(ctx, signals)

		require.Equal(t, "starting", <-stdout)

		for i := 0; i < 3; i++ {
			signals <- syscall.SIGHUP
			assert.Equal(t, "received: SIGHUP", <-stdout)
		}

		require.Equal(t, "exiting", <-stdout)
		require.Equal(t, "true", <-stdout)
	})

	t.Run("graceful termination", func(t *testing.T) {
		ctx := log.IntoContext(context.Background(), testr.New(t))
		ctx, cancel := context.WithCancel(ctx)
		signals := make(chan os.Signal)

		stdout := run(ctx, signals)

		require.Equal(t, "starting", <-stdout)

		cancel()

		require.Equal(t, "received: SIGTERM", <-stdout)
		require.Equal(t, "exiting", <-stdout)

		// Context cancellation doesn't result in requesting of a retry.
		require.Equal(t, "false", <-stdout)
	})
}
