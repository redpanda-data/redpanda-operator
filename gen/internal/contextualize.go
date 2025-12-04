package internal

import (
	"errors"
	"go/scanner"
	"strconv"
	"strings"
)

func ContextualizeFormatErrors(data []byte, err error) string {
	var serr scanner.ErrorList
	if errors.As(err, &serr) {
		errContext := []string{}
		lines := strings.Split(string(data), "\n")

		for i, err := range serr {
			line := err.Pos.Line

			lineContext := []string{"[ERROR " + strconv.Itoa(i+1) + "]:\n"}
			if line-2 >= 0 {
				lineContext = append(lineContext, lines[line-2])
			}
			lineContext = append(lineContext, lines[line-1])
			if line < len(lines) {
				lineContext = append(lineContext, lines[line])
			}
			errContext = append(errContext, strings.Join(lineContext, "\n"))
		}
		return strings.Join(errContext, "\n\n")
	}

	return ""
}
