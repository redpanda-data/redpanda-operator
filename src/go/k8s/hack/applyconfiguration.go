// go:build applyconfiguration
// build applyconfiguration

package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path"
)

var (
	fileStructs = map[string]string{
		"redpanda/v1alpha2/user.go": "User",
	}
)

func main() {
	for acpath, name := range fileStructs {
		path := path.Join("api/applyconfiguration", acpath)

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		buffer := bytes.NewBuffer(data)

		writeLine := func(line string) {
			_, err = buffer.WriteString(line + "\n")
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(1)
			}
		}

		writeLines := func(lines ...string) {
			writeLine("")
			for _, line := range lines {
				writeLine(line)
			}
		}

		// GetName
		writeLines(
			fmt.Sprintf("func (ac * %[1]sApplyConfiguration) GetName() string {", name),
			"ac.ensureObjectMetaApplyConfigurationExists()",
			"if ac.Name == nil {",
			"return \"\"",
			"}",
			"return *ac.Name",
			"}",
		)

		// GetNamespace
		writeLines(
			fmt.Sprintf("func (ac * %[1]sApplyConfiguration) GetNamespace() string {", name),
			"ac.ensureObjectMetaApplyConfigurationExists()",
			"if ac.Namespace == nil {",
			"return \"\"",
			"}",
			"return *ac.Namespace",
			"}",
		)

		data, err = format.Source(buffer.Bytes())
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		if err := os.WriteFile(path, data, 0644); err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}
	}
}
