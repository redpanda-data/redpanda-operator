package main

import (
	"github.com/spf13/cobra"

	"github.com/redpanda-data/redpanda-operator/gen/partial"
	"github.com/redpanda-data/redpanda-operator/gen/pipeline"
	"github.com/redpanda-data/redpanda-operator/gen/schema"
	"github.com/redpanda-data/redpanda-operator/gen/status"
)

func main() {
	root := cobra.Command{
		Use:  "gen",
		Long: "gen is the hub module for hosting various file generation tasks within this repo.",
	}

	root.AddCommand(
		partial.Cmd(),
		pipeline.Cmd(),
		schema.Cmd(),
		status.Cmd(),
	)

	if err := root.Execute(); err != nil {
		panic(err)
	}
}
