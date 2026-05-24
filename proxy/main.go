package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"

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

type Balancer struct {
	backends []ContainerBackend

	next  atomic.Uint64
	proxy *httputil.ReverseProxy
}

func (b *Balancer) pick() *url.URL {
	i := b.next.Add(1)
	return b.backends[int(i)%len(b.backends)].URL
}

func (b *Balancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.proxy.ServeHTTP(w, r)
}

func NewBalancer(backends []container.Summary) *Balancer {
	b := &Balancer{}
	for _, u := range backends {

		url, err := prepareUrlFromContainer(u)
		if err != nil {
			panic(err)
		}
		backend := ContainerBackend{u.ID, url}
		b.backends = append(b.backends, backend)
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

func getContainers(options container.ListOptions) []container.Summary {
	apiClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}
	defer apiClient.Close()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "service=hello")
	containers, err := apiClient.ContainerList(context.Background(), options)
	if err != nil {
		panic(err)
	}
	return containers
}

func listenForEvents(cli *client.Client, options events.ListOptions) {
	msgChan, errChan := cli.Events(context.Background(), options)
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

	listOptions := container.ListOptions{Filters: filterArgs}

	containers := getContainers(listOptions)

	proxy := NewBalancer(containers)

	http.ListenAndServe(":8000", proxy)
}
