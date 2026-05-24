package main

import (
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sync/atomic"
)

type Backends struct {
	Backends []string `json:"backends"`
}
type Balancer struct {
	backends []*url.URL
	next     atomic.Uint64
	proxy    *httputil.ReverseProxy
}

func (b *Balancer) pick() *url.URL {
	i := b.next.Add(1)
	return b.backends[int(i)%len(b.backends)]
}

func (b *Balancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.proxy.ServeHTTP(w, r)
}
func NewBalancer(backends []string) *Balancer {
	b := &Balancer{}
	for _, u := range backends {
		u, err := url.Parse(u)
		if err != nil {
			panic(err)
		}
		b.backends = append(b.backends, u)
	}

	b.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := b.pick()
			pr.SetURL(target)
			pr.Out.Host = target.Host
		},
	}
	return b
}

func main() {
	data, err := os.ReadFile("config.json")
	if err != nil {
		panic(err)
	}
	var backends Backends
	if err := json.Unmarshal(data, &backends); err != nil {
		panic(err)
	}
	b := NewBalancer(backends.Backends)
	http.ListenAndServe(":8000", b)
}
