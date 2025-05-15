package otelutil_test

import (
	"testing"

	"github.com/redpanda-data/redpanda-operator/pkg/otelutil"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/log"
	"github.com/redpanda-data/redpanda-operator/pkg/otelutil/trace"
)

func TestMain(m *testing.M) {
	otelutil.TestMain(m)
}

func TestBar(t *testing.T) {
	ctx := trace.Test(t)

	for i := 0; i < 10; i++ {
		log.Info(ctx, "hello world")
	}
}
