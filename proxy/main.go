package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// used for string parsing
type Backends struct {
	Backends []string `json:"backends"`
}

type ContainerBackend struct {
	ContainerID string
	URL         *url.URL
}

type ContainerPool struct {
	backends []ContainerBackend
	mu       sync.RWMutex
}

func (p *ContainerPool) AddBackend(backend ContainerBackend) {
	p.mu.Lock()
	defer p.mu.Unlock()
	log.Println("Adding backend:", backend.ContainerID)
	for _, b := range p.backends {
		if b.ContainerID == backend.ContainerID {
			return // Backend already exists, do not add again
		}
	}
	p.backends = append(p.backends, backend)
}

func (p *ContainerPool) RemoveBackend(containerID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	log.Println("Removing backend:", containerID)
	for i, backend := range p.backends {
		if backend.ContainerID == containerID {
			p.backends = append(p.backends[:i], p.backends[i+1:]...)
			return
		}
	}
}

type Balancer struct {
	pool *ContainerPool

	next  atomic.Uint64
	proxy *httputil.ReverseProxy
}

func (b *Balancer) pick() *url.URL {
	i := b.next.Add(1)
	return b.pool.backends[int(i)%len(b.pool.backends)].URL
}

func (b *Balancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.proxy.ServeHTTP(w, r)
}

// catch basic start/stop actions to update the pool of backends in the load balancer
func (b *Balancer) UpdatePool(ctx context.Context, cli *client.Client, event events.Message) {
	switch event.Action {
	case events.ActionStart:
		// get container details
		containerJSON, err := cli.ContainerInspect(ctx, event.Actor.ID)
		if err != nil {
			log.Printf("Error inspecting container %s: %v", event.Actor.ID, err)
			return
		}
		ports := containerJSON.Config.ExposedPorts
		if len(ports) == 0 {
			log.Printf("Container %s has no exposed ports, skipping", event.Actor.ID)
			return
		}

		// for now, just pick the first port
		var port int
		for k, _ := range ports {
			port = k.Int()
			break
		}
		url, err := PrepareUrlFromContainer(containerJSON.Name, port)
		if err != nil {
			log.Printf("Error preparing URL for container %s: %v", event.Actor.ID, err)
			return
		}
		backend := ContainerBackend{event.Actor.ID[:12], url}
		b.pool.AddBackend(backend)
	case events.ActionStop:
		b.pool.RemoveBackend(event.Actor.ID[:12])
	}
}

func NewBalancer(backends []container.Summary) *Balancer {
	pool := ContainerPool{}
	b := &Balancer{pool: &pool}
	for _, u := range backends {
		if len(u.Names) == 0 {
			continue
		}
		if len(u.Ports) == 0 {
			continue
		}
		name := u.Names[0]
		port := u.Ports[0].PrivatePort
		url, err := PrepareUrlFromContainer(name, int(port))
		if err != nil {
			panic(err)
		}
		backend := ContainerBackend{u.ID[:12], url}
		pool.AddBackend(backend)
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

func GetContainers(ctx context.Context, cli *client.Client, options container.ListOptions) []container.Summary {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "service=hello")
	containers, err := cli.ContainerList(ctx, options)
	if err != nil {
		panic(err)
	}
	return containers
}

func ListenForEvents(ctx context.Context, cli *client.Client, options events.ListOptions, balancer *Balancer) {
	msgChan, errChan := cli.Events(ctx, options)
	for {
		select {
		case msg := <-msgChan:
			balancer.UpdatePool(ctx, cli, msg)
		case err := <-errChan:
			if err != nil {
				log.Fatalf("Event stream error: %v", err)
			}
			return
		case <-ctx.Done():
			log.Println("Terminating container event listener")
		}
	}
}

func PrepareUrlFromContainer(name string, port int) (*url.URL, error) {
	name = strings.ReplaceAll(name, "/", "")
	url_str := fmt.Sprintf("http://%s:%d", name, port)
	url, err := url.Parse(url_str)
	if err != nil {
		return nil, err
	}
	return url, err
}

func main() {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	defer cli.Close()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "service=hello")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	listOptions := container.ListOptions{Filters: filterArgs}
	// get containers at runtime
	containers := GetContainers(ctx, cli, listOptions)
	// initiate load balancer with said containers
	proxy := NewBalancer(containers)

	go ListenForEvents(ctx, cli, events.ListOptions{Filters: filterArgs}, proxy)
	http.ListenAndServe(":8000", proxy)
}
