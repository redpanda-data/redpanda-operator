package adminv2

import (
	adminv2connect "buf.build/gen/go/redpandadata/core/connectrpc/go/redpanda/core/admin/v2/adminv2connect"
	"connectrpc.com/connect"
)

type Client struct {
	client *wrappedClient
}

func (c *Client) Brokers(opts ...connect.ClientOption) adminv2connect.BrokerServiceClient {
	return adminv2connect.NewBrokerServiceClient(c.client, "/v2", opts...)
}

func (c *Client) ShadowLinks(opts ...connect.ClientOption) adminv2connect.ShadowLinkServiceClient {
	return adminv2connect.NewShadowLinkServiceClient(c.client, "/v2", opts...)
}

func (c *Client) Close() {
	c.client.close()
}
