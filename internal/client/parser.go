package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/traefik/paerser/parser"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
	"github.com/traefik/traefik/v3/pkg/tls"
)

// BuildTraefikConfig parses labels into a Traefik dynamic config and returns it as JSON
func BuildTraefikConfig(containers []container.Summary, hostIP string) ([]byte, error) {
	rootConfig := &dynamic.Configuration{}
	for _, c := range containers {
		if c.Labels["traefik.enable"] != "true" {
			continue
		}

		containerName := c.Names[0][1:]
		slog.Debug("Processing container", "name", containerName)

		dyn := &dynamic.Configuration{}
		if err := parser.Decode(
			c.Labels,
			dyn,
			parser.DefaultRootName,
			"traefik.http",
			"traefik.tcp",
			"traefik.udp",
			"traefik.tls",
		); err != nil {
			slog.Error("Failed to parse traefik labels", "container", containerName, "error", err)
			continue
		}

		defaultPort, portMap := extractContainerPorts(c)
		if len(portMap) == 0 {
			slog.Debug("Container exposes no public ports, skipping", "container", containerName)
			continue // skip, container is unreachable from the central server
		}

		// Process HTTP
		if dyn.HTTP != nil {
			if rootConfig.HTTP == nil {
				rootConfig.HTTP = &dynamic.HTTPConfiguration{
					Routers:     make(map[string]*dynamic.Router),
					Services:    make(map[string]*dynamic.Service),
					Middlewares: make(map[string]*dynamic.Middleware),
				}
			}

			for name, r := range dyn.HTTP.Routers {
				if r.Service == "" {
					r.Service = name
				}
				rootConfig.HTTP.Routers[name] = r
				ensureHTTPService(dyn, r.Service)
			}
			for name, svc := range dyn.HTTP.Services {
				if svc.LoadBalancer != nil {
					processHTTPServers(svc.LoadBalancer, hostIP, defaultPort, portMap)
				}
				rootConfig.HTTP.Services[name] = svc
			}
			maps.Copy(rootConfig.HTTP.Middlewares, dyn.HTTP.Middlewares)
		}

		// Process TCP
		if dyn.TCP != nil {
			if rootConfig.TCP == nil {
				rootConfig.TCP = &dynamic.TCPConfiguration{
					Routers:     make(map[string]*dynamic.TCPRouter),
					Services:    make(map[string]*dynamic.TCPService),
					Middlewares: make(map[string]*dynamic.TCPMiddleware),
				}
			}

			for name, r := range dyn.TCP.Routers {
				if r.Service == "" {
					r.Service = name
				}
				rootConfig.TCP.Routers[name] = r
				ensureTCPService(dyn, r.Service)
			}
			for name, svc := range dyn.TCP.Services {
				if svc.LoadBalancer != nil {
					processTCPServers(svc.LoadBalancer, hostIP, defaultPort, portMap)
				}
				rootConfig.TCP.Services[name] = svc
			}
			maps.Copy(rootConfig.TCP.Middlewares, dyn.TCP.Middlewares)
		}

		// Process UDP
		if dyn.UDP != nil {
			if rootConfig.UDP == nil {
				rootConfig.UDP = &dynamic.UDPConfiguration{
					Routers:  make(map[string]*dynamic.UDPRouter),
					Services: make(map[string]*dynamic.UDPService),
				}
			}

			for name, r := range dyn.UDP.Routers {
				if r.Service == "" {
					r.Service = name
				}
				rootConfig.UDP.Routers[name] = r
				ensureUDPService(dyn, r.Service)
			}
			for name, svc := range dyn.UDP.Services {
				if svc.LoadBalancer != nil {
					processUDPServers(svc.LoadBalancer, hostIP, defaultPort, portMap)
				}
				rootConfig.UDP.Services[name] = svc
			}
		}

		// Process TLS
		if dyn.TLS != nil {
			if rootConfig.TLS == nil {
				rootConfig.TLS = &dynamic.TLSConfiguration{
					Options: make(map[string]tls.Options),
					Stores:  make(map[string]tls.Store),
				}
			}
			if len(dyn.TLS.Certificates) > 0 {
				rootConfig.TLS.Certificates = append(
					rootConfig.TLS.Certificates,
					dyn.TLS.Certificates...)
			}
			if dyn.TLS.Options != nil {
				maps.Copy(rootConfig.TLS.Options, dyn.TLS.Options)
			}
			if dyn.TLS.Stores != nil {
				maps.Copy(rootConfig.TLS.Stores, dyn.TLS.Stores)
			}
		}
	}

	return json.Marshal(rootConfig)
}

// Helpers

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

// HTTP Helpers

func ensureHTTPService(config *dynamic.Configuration, svcName string) {
	if config.HTTP.Services == nil {
		config.HTTP.Services = make(map[string]*dynamic.Service)
	}
	if _, ok := config.HTTP.Services[svcName]; !ok {
		config.HTTP.Services[svcName] = &dynamic.Service{
			LoadBalancer: &dynamic.ServersLoadBalancer{
				PassHostHeader: new(true), // default to true
			},
		}
	}
}

func processHTTPServers(
	lb *dynamic.ServersLoadBalancer,
	hostIP, defaultPort string,
	portMap map[string]string,
) {
	if len(lb.Servers) == 0 {
		lb.Servers = []dynamic.Server{{
			URL: fmt.Sprintf("http://%s:%s", hostIP, defaultPort),
		}}
		return
	}

	validServers := make([]dynamic.Server, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		fmt.Printf("srv: %#v\n", srv)
		mapped := portMap[srv.Port]
		if mapped == "" {
			mapped = defaultPort
		}
		if mapped == "" {
			continue // Skip invalid servers
		}

		scheme := srv.Scheme
		if scheme == "" {
			if strings.HasPrefix(srv.URL, "https://") {
				scheme = "https"
			} else if strings.HasPrefix(srv.URL, "h2c://") {
				scheme = "h2c"
			} else {
				scheme = "http"
			}
		}

		// Bake everything into the final URL
		srv.URL = fmt.Sprintf("%s://%s:%s", scheme, hostIP, mapped)

		// Clean up fields
		srv.Port = ""
		srv.Scheme = ""

		validServers = append(validServers, srv)
	}

	lb.Servers = validServers
}

// TCP Helpers

func ensureTCPService(config *dynamic.Configuration, svcName string) {
	if config.TCP.Services == nil {
		config.TCP.Services = make(map[string]*dynamic.TCPService)
	}
	if _, ok := config.TCP.Services[svcName]; !ok {
		config.TCP.Services[svcName] = &dynamic.TCPService{
			LoadBalancer: &dynamic.TCPServersLoadBalancer{},
		}
	}
}

func processTCPServers(
	lb *dynamic.TCPServersLoadBalancer,
	hostIP, defaultPort string,
	portMap map[string]string,
) {
	if len(lb.Servers) == 0 {
		lb.Servers = []dynamic.TCPServer{{Address: fmt.Sprintf("%s:%s", hostIP, defaultPort)}}
		return
	}
	validServers := make([]dynamic.TCPServer, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		mapped := portMap[srv.Port]
		if mapped == "" {
			mapped = defaultPort
		}
		if mapped == "" {
			continue
		}
		srv.Address = fmt.Sprintf("%s:%s", hostIP, mapped)
		srv.Port = ""

		validServers = append(validServers, srv)
	}

	lb.Servers = validServers
}

// UDP Helpers

func ensureUDPService(config *dynamic.Configuration, svcName string) {
	if config.UDP.Services == nil {
		config.UDP.Services = make(map[string]*dynamic.UDPService)
	}
	if _, ok := config.UDP.Services[svcName]; !ok {
		config.UDP.Services[svcName] = &dynamic.UDPService{
			LoadBalancer: &dynamic.UDPServersLoadBalancer{},
		}
	}
}

func processUDPServers(
	lb *dynamic.UDPServersLoadBalancer,
	hostIP, defaultPort string,
	portMap map[string]string,
) {
	if len(lb.Servers) == 0 {
		if defaultPort != "" {
			lb.Servers = []dynamic.UDPServer{{Address: fmt.Sprintf("%s:%s", hostIP, defaultPort)}}
		}
		return
	}

	validServers := make([]dynamic.UDPServer, 0, len(lb.Servers))
	for _, srv := range lb.Servers {
		mapped := portMap[srv.Port]
		if mapped == "" {
			mapped = defaultPort
		}
		if mapped == "" {
			continue
		}
		srv.Address = fmt.Sprintf("%s:%s", hostIP, mapped)
		srv.Port = ""

		validServers = append(validServers, srv)
	}

	lb.Servers = validServers
}
