package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/traefik/paerser/parser"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/tls"
)

// target is where one container's published ports are reachable from the gateway.
type target struct {
	hostIP      string
	defaultPort string
	portMap     map[string]string // private -> public
	hostMode    bool
	labels      map[string]string
}

// BuildTraefikConfig parses labels into a Traefik dynamic config and returns it as JSON.
func BuildTraefikConfig(containers []container.Summary, hostIP string) ([]byte, error) {
	root := &dynamic.Configuration{}
	for _, c := range containers {
		if c.Labels["traefik.enable"] != "true" || len(c.Names) == 0 {
			continue
		}

		name := strings.TrimPrefix(c.Names[0], "/")
		// Same as traefik's docker provider: no routes until the container is healthy.
		if c.Health != nil && (c.Health.Status == container.Starting || c.Health.Status == container.Unhealthy) {
			slog.Debug("Skipping container, not healthy", "name", name, "health", c.Health.Status)
			continue
		}
		slog.Debug("Processing container", "name", name)

		dyn := &dynamic.Configuration{}
		err := parser.Decode(c.Labels, dyn, parser.DefaultRootName, "traefik.http", "traefik.tcp", "traefik.udp",
			"traefik.tls")
		if err != nil {
			slog.Error("Failed to parse traefik labels", "container", name, "error", err)
			continue
		}

		defaultPort, portMap := extractContainerPorts(c)
		t := target{
			hostIP:      hostIP,
			defaultPort: defaultPort,
			portMap:     portMap,
			hostMode:    c.HostConfig.NetworkMode == "host",
			labels:      c.Labels,
		}
		addHTTP(root, dyn.HTTP, t)
		addTCP(root, dyn.TCP, t)
		addUDP(root, dyn.UDP, t)
		addTLS(root, dyn.TLS)
	}

	localizeDockerRefs(root)
	return json.Marshal(root)
}

func addHTTP(root *dynamic.Configuration, src *dynamic.HTTPConfiguration, t target) {
	if src == nil {
		return
	}
	if root.HTTP == nil {
		root.HTTP = &dynamic.HTTPConfiguration{
			Routers:     make(map[string]*dynamic.Router),
			Services:    make(map[string]*dynamic.Service),
			Middlewares: make(map[string]*dynamic.Middleware),
		}
	}
	dst := root.HTTP

	for name, r := range src.Routers {
		r.Service = serviceRef(r.Service, name)
		dst.Routers[name] = r
		if !isProviderRef(r.Service) {
			if src.Services == nil {
				src.Services = make(map[string]*dynamic.Service)
			}
			if _, ok := src.Services[r.Service]; !ok {
				src.Services[r.Service] = &dynamic.Service{
					LoadBalancer: &dynamic.ServersLoadBalancer{PassHostHeader: new(true)},
				}
			}
		}
	}
	for name, svc := range src.Services {
		if svc.LoadBalancer == nil {
			continue
		}
		processHTTPServers(svc.LoadBalancer, t, t.servicePort("http", name))
		if len(svc.LoadBalancer.Servers) > 0 {
			dst.Services[name] = svc
		}
	}
	for name, r := range dst.Routers {
		if _, ok := dst.Services[r.Service]; !ok && !isProviderRef(r.Service) {
			delete(dst.Routers, name)
		}
	}
	maps.Copy(dst.Middlewares, src.Middlewares)
}

func addTCP(root *dynamic.Configuration, src *dynamic.TCPConfiguration, t target) {
	if src == nil {
		return
	}
	if root.TCP == nil {
		root.TCP = &dynamic.TCPConfiguration{
			Routers:     make(map[string]*dynamic.TCPRouter),
			Services:    make(map[string]*dynamic.TCPService),
			Middlewares: make(map[string]*dynamic.TCPMiddleware),
		}
	}
	dst := root.TCP

	for name, r := range src.Routers {
		r.Service = serviceRef(r.Service, name)
		dst.Routers[name] = r
		if !isProviderRef(r.Service) {
			if src.Services == nil {
				src.Services = make(map[string]*dynamic.TCPService)
			}
			if _, ok := src.Services[r.Service]; !ok {
				src.Services[r.Service] = &dynamic.TCPService{LoadBalancer: &dynamic.TCPServersLoadBalancer{}}
			}
		}
	}
	for name, svc := range src.Services {
		if svc.LoadBalancer == nil {
			continue
		}
		processTCPServers(svc.LoadBalancer, t, t.servicePort("tcp", name))
		if len(svc.LoadBalancer.Servers) > 0 {
			dst.Services[name] = svc
		}
	}
	for name, r := range dst.Routers {
		if _, ok := dst.Services[r.Service]; !ok && !isProviderRef(r.Service) {
			delete(dst.Routers, name)
		}
	}
	maps.Copy(dst.Middlewares, src.Middlewares)
}

func addUDP(root *dynamic.Configuration, src *dynamic.UDPConfiguration, t target) {
	if src == nil {
		return
	}
	if root.UDP == nil {
		root.UDP = &dynamic.UDPConfiguration{
			Routers:  make(map[string]*dynamic.UDPRouter),
			Services: make(map[string]*dynamic.UDPService),
		}
	}
	dst := root.UDP

	for name, r := range src.Routers {
		r.Service = serviceRef(r.Service, name)
		dst.Routers[name] = r
		if !isProviderRef(r.Service) {
			if src.Services == nil {
				src.Services = make(map[string]*dynamic.UDPService)
			}
			if _, ok := src.Services[r.Service]; !ok {
				src.Services[r.Service] = &dynamic.UDPService{LoadBalancer: &dynamic.UDPServersLoadBalancer{}}
			}
		}
	}
	for name, svc := range src.Services {
		if svc.LoadBalancer == nil {
			continue
		}
		processUDPServers(svc.LoadBalancer, t, t.servicePort("udp", name))
		if len(svc.LoadBalancer.Servers) > 0 {
			dst.Services[name] = svc
		}
	}
	for name, r := range dst.Routers {
		if _, ok := dst.Services[r.Service]; !ok && !isProviderRef(r.Service) {
			delete(dst.Routers, name)
		}
	}
}

func addTLS(root *dynamic.Configuration, src *dynamic.TLSConfiguration) {
	if src == nil {
		return
	}
	if root.TLS == nil {
		root.TLS = &dynamic.TLSConfiguration{
			Options: make(map[string]tls.Options),
			Stores:  make(map[string]tls.Store),
		}
	}
	root.TLS.Certificates = append(root.TLS.Certificates, src.Certificates...)
	maps.Copy(root.TLS.Options, src.Options)
	maps.Copy(root.TLS.Stores, src.Stores)
}

// serviceRef defaults to the router name and drops an @docker suffix, since services
// defined by this host's labels are served through tether's http provider.
func serviceRef(service, router string) string {
	if service == "" {
		return router
	}
	return strings.TrimSuffix(service, "@docker")
}

// isProviderRef reports whether the name points at another provider, e.g. api@internal or auth@file.
func isProviderRef(name string) bool {
	return strings.Contains(name, "@")
}

// localizeDockerRefs rewrites name@docker middleware references to a plain name when this host
// defines that middleware, since tether serves it as name@http. Unknown ones are left alone,
// they may live in the gateway's own docker provider.
func localizeDockerRefs(cfg *dynamic.Configuration) {
	if cfg.HTTP != nil {
		for _, r := range cfg.HTTP.Routers {
			localizeRefs(r.Middlewares, cfg.HTTP.Middlewares)
		}
		for _, m := range cfg.HTTP.Middlewares {
			if m.Chain != nil {
				localizeRefs(m.Chain.Middlewares, cfg.HTTP.Middlewares)
			}
		}
	}
	if cfg.TCP != nil {
		for _, r := range cfg.TCP.Routers {
			localizeRefs(r.Middlewares, cfg.TCP.Middlewares)
		}
	}
}

func localizeRefs[V any](refs []string, defined map[string]V) {
	for i, ref := range refs {
		if name, ok := strings.CutSuffix(ref, "@docker"); ok {
			if _, exists := defined[name]; exists {
				refs[i] = name
			}
		}
	}
}

func extractContainerPorts(c container.Summary) (string, map[string]string) {
	portMap := make(map[string]string)
	var publicPorts []int

	for _, p := range c.Ports {
		if p.PublicPort != 0 {
			pub := strconv.Itoa(int(p.PublicPort))
			priv := strconv.Itoa(int(p.PrivatePort))
			portMap[priv] = pub
			publicPorts = append(publicPorts, int(p.PublicPort))
		}
	}

	defaultPort := ""
	if len(publicPorts) > 0 {
		slices.Sort(publicPorts)
		defaultPort = strconv.Itoa(publicPorts[0])
	}

	return defaultPort, portMap
}

// servicePort returns the port label for a service, matched case insensitively like traefik does.
func (t target) servicePort(protocol, service string) string {
	key := fmt.Sprintf("traefik.%s.services.%s.loadbalancer.server.port", protocol, service)
	for k, v := range t.labels {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// resolvePort decides which port to use for a server given the container's
// published ports. Falls back to the declared port when it is already public,
// when the container runs in host mode, or when there is nothing to fall back to.
func (t target) resolvePort(port string) string {
	if mapped := t.portMap[port]; mapped != "" {
		return mapped
	}
	for _, pub := range t.portMap {
		if pub == port {
			return port
		}
	}
	if port != "" && (t.hostMode || t.defaultPort == "") {
		return port
	}
	return t.defaultPort
}

func processHTTPServers(lb *dynamic.ServersLoadBalancer, t target, explicitPort string) {
	switch {
	case len(lb.Servers) == 0 && explicitPort != "":
		lb.Servers = []dynamic.Server{{Port: explicitPort}}
	case len(lb.Servers) == 0 && t.defaultPort != "":
		lb.Servers = []dynamic.Server{{URL: "http://" + net.JoinHostPort(t.hostIP, t.defaultPort)}}
		return
	case len(lb.Servers) == 0:
		return
	case explicitPort != "":
		lb.Servers[0].Port = explicitPort
	}

	valid := make([]dynamic.Server, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		mapped := t.resolvePort(srv.Port)
		if mapped == "" {
			continue
		}

		scheme := srv.Scheme
		if scheme == "" {
			switch {
			case strings.HasPrefix(srv.URL, "https://"):
				scheme = "https"
			case strings.HasPrefix(srv.URL, "h2c://"):
				scheme = "h2c"
			default:
				scheme = "http"
			}
		}
		srv.URL = scheme + "://" + net.JoinHostPort(t.hostIP, mapped)
		srv.Port = ""
		srv.Scheme = ""
		valid = append(valid, srv)
	}
	lb.Servers = valid
}

//nolint:dupl // processUDPServers is structurally identical, traefik has no shared TCP/UDP server type
func processTCPServers(lb *dynamic.TCPServersLoadBalancer, t target, explicitPort string) {
	switch {
	case len(lb.Servers) == 0 && explicitPort != "":
		lb.Servers = []dynamic.TCPServer{{Port: explicitPort}}
	case len(lb.Servers) == 0 && t.defaultPort != "":
		lb.Servers = []dynamic.TCPServer{{Address: net.JoinHostPort(t.hostIP, t.defaultPort)}}
		return
	case len(lb.Servers) == 0:
		return
	case explicitPort != "":
		lb.Servers[0].Port = explicitPort
	}

	valid := make([]dynamic.TCPServer, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		mapped := t.resolvePort(srv.Port)
		if mapped == "" {
			continue
		}
		srv.Address = net.JoinHostPort(t.hostIP, mapped)
		srv.Port = ""
		valid = append(valid, srv)
	}
	lb.Servers = valid
}

//nolint:dupl // see processTCPServers
func processUDPServers(lb *dynamic.UDPServersLoadBalancer, t target, explicitPort string) {
	switch {
	case len(lb.Servers) == 0 && explicitPort != "":
		lb.Servers = []dynamic.UDPServer{{Port: explicitPort}}
	case len(lb.Servers) == 0 && t.defaultPort != "":
		lb.Servers = []dynamic.UDPServer{{Address: net.JoinHostPort(t.hostIP, t.defaultPort)}}
		return
	case len(lb.Servers) == 0:
		return
	case explicitPort != "":
		lb.Servers[0].Port = explicitPort
	}

	valid := make([]dynamic.UDPServer, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		mapped := t.resolvePort(srv.Port)
		if mapped == "" {
			continue
		}
		srv.Address = net.JoinHostPort(t.hostIP, mapped)
		srv.Port = ""
		valid = append(valid, srv)
	}
	lb.Servers = valid
}
