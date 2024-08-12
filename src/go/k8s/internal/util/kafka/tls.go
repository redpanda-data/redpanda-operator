package kafka

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/redpanda-data/helm-charts/pkg/redpanda"
)

func wrapTLSDialer(dialer redpanda.DialContextFunc, config *tls.Config) redpanda.DialContextFunc {
	return func(ctx context.Context, network, host string) (net.Conn, error) {
		conn, err := dialer(ctx, network, host)
		if err != nil {
			return nil, err
		}
		return tls.Client(conn, config), nil
	}
}
