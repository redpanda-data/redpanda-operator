package helm

import (
	"fmt"
	"maps"
	"net/http"
	"sort"
	"sync"
	"testing/fstest"
	"time"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

// Repository is an [http.Handler] that acts as a semi functional helm
// repository. It is suitable for use with `helm repo <cmd>` and as a source
// for Flux in tests.
type Repository struct {
	*http.ServeMux

	mu    sync.Mutex
	index *repo.IndexFile
	fs    fstest.MapFS
}

var _ http.Handler = &Repository{}

func NewRepository() *Repository {
	r := &Repository{
		ServeMux: http.NewServeMux(),
		index:    repo.NewIndexFile(),
		fs:       fstest.MapFS{},
	}

	r.ServeMux.HandleFunc("/index.yaml", r.indexHandler)
	r.ServeMux.Handle("/", http.FileServer(http.FS(r.fs)))

	return r
}

// AddChart adds a
func (r *Repository) AddChart(metadata chart.Metadata, tgz []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := metadata.Name
	version := metadata.Version
	archiveName := fmt.Sprintf("%s-%s.tgz", name, version)

	versions := r.index.Entries[name]

	versions = append(versions, &repo.ChartVersion{
		Metadata: &metadata,
		URLs: []string{
			"/" + archiveName,
		},
		Created: time.Now(),
	})

	sort.Sort(versions)

	r.index.Entries[name] = versions
	r.fs[archiveName] = &fstest.MapFile{Data: tgz}
}

func (r *Repository) indexHandler(rw http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rw.Header().Add("Content-Type", "application/yaml")

	// Because we don't know the host of our server and helm expects URLs to be
	// fully qualified, we clone the index and reflect back the host of the URL
	// that was used to access this endpoint.
	index := *r.index
	index.Entries = maps.Clone(index.Entries)

	for chart, versions := range index.Entries {
		updated := make(repo.ChartVersions, len(versions))
		for i, version := range versions {
			v := *version
			v.URLs = mapSlice(v.URLs, func(url string) string {
				u := *req.URL
				u.Path = url
				return u.String()
			})

			updated[i] = &v
		}
		index.Entries[chart] = updated
	}

	out, err := yaml.Marshal(r.index)
	if err != nil {
		panic(err)
	}

	_, _ = rw.Write(out)
}

func mapSlice[S ~[]E, E any, T any](s S, fn func(E) T) []T {
	out := make([]T, len(s))
	for i, el := range s {
		out[i] = fn(el)
	}
	return out
}
