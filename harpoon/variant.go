package framework

import (
	"strings"

	messages "github.com/cucumber/messages/go/v21"
)

func featureVariant(tags []*messages.Tag) string {
	for _, tag := range tags {
		name := strings.TrimPrefix(tag.Name, "@")
		if strings.HasPrefix(name, "variant:") {
			return strings.TrimPrefix(name, "variant:")
		}
	}
	return ""
}
