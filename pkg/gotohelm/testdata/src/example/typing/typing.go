package typing

import (
	"github.com/redpanda-data/redpanda-operator/pkg/gotohelm/helmette"
)

func Typing(dot *helmette.Dot) map[string]any {
	return map[string]any{
		"zeros":   zeros(),
		"numbers": numbers(),
		// "settingFields":     settingFields(),
		"compileMe":         compileMe(),
		"typeTesting":       typeTesting(dot),
		"typeAssertions":    typeSwitching(dot),
		"typeSwitching":     typeSwitching(dot),
		"nestedFieldAccess": nestedFieldAccess(),
	}
}
