package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
)

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
	backends := []string{
		"http://localhost:9000",
		"http://localhost:9001",
		"http://localhost:9002",
	}
	b := NewBalancer(backends)
	http.ListenAndServe(":8000", b)
}
