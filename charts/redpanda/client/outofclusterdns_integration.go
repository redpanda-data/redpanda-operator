//go:build integration || acceptance

package client

func init() {
	permitOutOfClusterDNS = true
}
