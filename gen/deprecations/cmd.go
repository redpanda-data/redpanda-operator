package deprecations

import (
	_ "embed"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

const DeprecationPrefix = "Deprecated"

var (
	testGenerator *template.Template
	//go:embed templates/test.tpl
	testTemplate string
)

func init() {
	testGenerator = template.Must(template.New("tests").Parse(testTemplate))
}

type DeprecationConfig struct {
	Directory   string
	PackageName string
	OutputFile  string
	Verbose     bool
}

type fieldRef struct {
	GoPath   []string // Go field names e.g. [Spec ClusterSpec DeprecatedFullNameOverride]
	JsonPath []string // JSON names e.g. [spec clusterSpec fullNameOverride]
	TypeExpr ast.Expr
}

type objSpec struct {
	Name     string
	Literal  string
	Warnings []string
}

// Debugf emits debug output when the config's Verbose flag is set.
func (c DeprecationConfig) Debugf(format string, a ...interface{}) {
	if !c.Verbose {
		return
	}
	fmt.Fprintf(os.Stderr, format, a...)
}

func Cmd() *cobra.Command {
	var config DeprecationConfig

	cmd := &cobra.Command{
		Use:     "deprecations",
		Short:   "Generate tests for deprecated API fields",
		Example: "gen deprecations --directory ./operator/api/redpanda/v1alpha2",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Render(config)
		},
	}

	cmd.Flags().StringVar(&config.Directory, "directory", ".", "The directory to scan for deprecated fields")
	cmd.Flags().StringVar(&config.PackageName, "package", "", "The name of the package, if not specified we try and figure it out dynamically")
	cmd.Flags().StringVar(&config.OutputFile, "output-file", "zz_generated.deprecations_test.go", "The name of the file to output in the given directory")
	cmd.Flags().BoolVarP(&config.Verbose, "verbose", "v", false, "Enable debug output.")

	return cmd
}

func Render(config DeprecationConfig) error {
	dir := config.Directory

	if config.PackageName == "" {
		config.PackageName = strings.Trim(filepath.Base(dir), ".")
		config.PackageName = strings.Trim(filepath.Base(dir), "/")
	}

	if config.PackageName == "" {
		return fmt.Errorf("could not determine package name")
	}

	parser := NewParser(config)

	if err := parser.Parse(); err != nil {
		return err
	}

	contents, err := parser.Compile()
	if err != nil {
		return err
	}

	outPath := filepath.Join(dir, config.OutputFile)
	if err := os.WriteFile(outPath, contents, 0644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Printf("wrote %s\n", outPath)

	return nil
}
