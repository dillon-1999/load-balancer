package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
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

func NewBalancer(backends []container.Summary) *Balancer {
	pool := ContainerPool{}
	b := &Balancer{pool: &pool}
	for _, u := range backends {
		url, err := prepareUrlFromContainer(u)
		if err != nil {
			panic(err)
		}
		backend := ContainerBackend{u.ID[:12], url}
		pool.AddBackend(backend)
	}

	b.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			target := b.pick()
			fmt.Println(target)
			pr.SetURL(target)
			pr.Out.Host = target.Host
		},
	}
	return b
}

func getContainers(ctx context.Context, options container.ListOptions) []container.Summary {
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	defer apiClient.Close()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "service=hello")
	containers, err := apiClient.ContainerList(ctx, options)
	if err != nil {
		panic(err)
	}
	return containers
}

func listenForEvents(ctx context.Context, cli *client.Client, options events.ListOptions) {
	msgChan, errChan := cli.Events(ctx, options)
	for {
		select {
		case msg := <-msgChan:
			fmt.Printf("Event: %s | Container: %s | Image: %s\n",
				msg.Action, msg.Actor.ID[:12], msg.Actor.Attributes["image"])
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

func prepareUrlFromContainer(c container.Summary) (*url.URL, error) {
	if len(c.Names) == 0 {
		return nil, fmt.Errorf("no name provided in container summary for container %s", c.ID)
	}
	name := strings.ReplaceAll(c.Names[0], "/", "")
	if len(c.Ports) == 0 {
		fmt.Errorf("no port open for container %s", c.ID)
	}
	port := c.Ports[0]
	url_str := fmt.Sprintf("http://%s:%d", name, port.PrivatePort)
	fmt.Println(url_str)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	listOptions := container.ListOptions{Filters: filterArgs}
	// get containers at runtime
	containers := getContainers(ctx, listOptions)
	// initiate load balancer with said containers
	proxy := NewBalancer(containers)

	go listenForEvents(ctx, cli, events.ListOptions{Filters: filterArgs})
	http.ListenAndServe(":8000", proxy)
}
