package files

import "github.com/redpanda-data/redpanda-operator/gotohelm/helmette"

func Files(dot *helmette.Dot) []any {
	return []any{
		dot.Files.Get("hello.txt"),
		dot.Files.GetBytes("hello.txt"),
		dot.Files.Lines("something.yaml"),
		dot.Files.Get("doesntexist"),
		dot.Files.GetBytes("doesntexist"),
		dot.Files.Lines("doesntexist"),
	}
}
