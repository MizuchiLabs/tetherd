package client

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/traefik/v3/pkg/config/dynamic"
)

const hostIP = "10.0.0.5"

type ports map[uint16]uint16 // private -> public

func newContainer(name string, p ports, labels map[string]string) container.Summary {
	c := container.Summary{Names: []string{"/" + name}, Labels: map[string]string{"traefik.enable": "true"}}
	maps.Copy(c.Labels, labels)
	for priv, pub := range p {
		c.Ports = append(c.Ports, container.PortSummary{PrivatePort: priv, PublicPort: pub, Type: "tcp"})
	}
	return c
}

func build(t *testing.T, containers ...container.Summary) *dynamic.Configuration {
	t.Helper()
	data, err := BuildTraefikConfig(containers, hostIP)
	require.NoError(t, err)
	cfg := &dynamic.Configuration{}
	require.NoError(t, json.Unmarshal(data, cfg))
	return cfg
}

func serverURL(t *testing.T, cfg *dynamic.Configuration, service string) string {
	t.Helper()
	require.NotNil(t, cfg.HTTP, "http config missing")
	svc, ok := cfg.HTTP.Services[service]
	require.True(t, ok, "service %q missing", service)
	require.Len(t, svc.LoadBalancer.Servers, 1)
	return svc.LoadBalancer.Servers[0].URL
}

// The scenarios mirror compose.yml.
func TestHTTPPortResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ports  ports
		labels map[string]string
		want   string
	}{
		{"lowest public port by default", ports{80: 8080, 81: 8090}, nil, "http://10.0.0.5:8080"},
		{"private port maps to public", ports{80: 8081, 8080: 8082}, map[string]string{
			"traefik.http.services.app.loadbalancer.server.port": "80",
		}, "http://10.0.0.5:8081"},
		{"public port used directly", ports{80: 8083, 8080: 8084}, map[string]string{
			"traefik.http.services.app.loadbalancer.server.port": "8084",
		}, "http://10.0.0.5:8084"},
		{"unknown port falls back to default", ports{80: 8088}, map[string]string{
			"traefik.http.services.app.loadbalancer.server.port": "9999",
		}, "http://10.0.0.5:8088"},
		{"scheme is kept", ports{443: 8443}, map[string]string{
			"traefik.http.services.app.loadbalancer.server.scheme": "https",
		}, "https://10.0.0.5:8443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			labels := map[string]string{"traefik.http.routers.app.rule": "Host(`app.local`)"}
			maps.Copy(labels, tt.labels)
			cfg := build(t, newContainer("app", tt.ports, labels))
			assert.Equal(t, tt.want, serverURL(t, cfg, "app"))
			assert.Equal(t, "app", cfg.HTTP.Routers["app"].Service)
		})
	}
}

func TestHostModeTrustsExplicitPort(t *testing.T) {
	t.Parallel()
	c := newContainer("hostmode", nil, map[string]string{
		"traefik.http.routers.hostmode.rule":                      "Host(`hostmode.local`)",
		"traefik.http.services.hostmode.loadbalancer.server.port": "8087",
	})
	c.HostConfig.NetworkMode = "host"
	assert.Equal(t, "http://10.0.0.5:8087", serverURL(t, build(t, c), "hostmode"))
}

func TestMultipleServicesPerContainer(t *testing.T) {
	t.Parallel()
	cfg := build(t, newContainer("multi", ports{80: 8085, 81: 8086}, map[string]string{
		"traefik.http.routers.multi1.rule":                      "Host(`multi1.local`)",
		"traefik.http.routers.multi1.service":                   "multi1",
		"traefik.http.services.multi1.loadbalancer.server.port": "80",
		"traefik.http.routers.multi2.rule":                      "Host(`multi2.local`)",
		"traefik.http.routers.multi2.service":                   "multi2",
		"traefik.http.services.multi2.loadbalancer.server.port": "81",
	}))
	assert.Equal(t, "http://10.0.0.5:8085", serverURL(t, cfg, "multi1"))
	assert.Equal(t, "http://10.0.0.5:8086", serverURL(t, cfg, "multi2"))
}

func TestRouterWithoutReachablePortIsDropped(t *testing.T) {
	t.Parallel()
	cfg := build(t, newContainer("noports", nil, map[string]string{
		"traefik.http.routers.noports.rule": "Host(`noports.local`)",
	}))
	if cfg.HTTP != nil {
		assert.Empty(t, cfg.HTTP.Routers)
	}
}

func TestTCPAndUDP(t *testing.T) {
	t.Parallel()
	cfg := build(t, newContainer("db", ports{5432: 15432, 53: 1053}, map[string]string{
		"traefik.tcp.routers.db.rule":                       "HostSNI(`*`)",
		"traefik.tcp.services.db.loadbalancer.server.port":  "5432",
		"traefik.udp.routers.dns.entrypoints":               "dns",
		"traefik.udp.services.dns.loadbalancer.server.port": "53",
	}))
	require.NotNil(t, cfg.TCP)
	assert.Equal(t, "10.0.0.5:15432", cfg.TCP.Services["db"].LoadBalancer.Servers[0].Address)
	require.NotNil(t, cfg.UDP)
	assert.Equal(t, "10.0.0.5:1053", cfg.UDP.Services["dns"].LoadBalancer.Servers[0].Address)
}

func TestUnhealthyContainersAreSkipped(t *testing.T) {
	t.Parallel()
	labels := map[string]string{"traefik.http.routers.app.rule": "Host(`app.local`)"}
	for _, status := range []container.HealthStatus{container.Starting, container.Unhealthy} {
		c := newContainer("app", ports{80: 8080}, labels)
		c.Health = &container.HealthSummary{Status: status}
		assert.Nil(t, build(t, c).HTTP, "container with health %q should be skipped", status)
	}

	c := newContainer("app", ports{80: 8080}, labels)
	c.Health = &container.HealthSummary{Status: container.Healthy}
	assert.Equal(t, "http://10.0.0.5:8080", serverURL(t, build(t, c), "app"))
}

func TestProviderServiceRefsAreKept(t *testing.T) {
	t.Parallel()
	cfg := build(t, newContainer("dash", ports{80: 8080}, map[string]string{
		"traefik.http.routers.dash.rule":    "Host(`dash.local`)",
		"traefik.http.routers.dash.service": "api@internal",
	}))
	require.NotNil(t, cfg.HTTP)
	require.Contains(t, cfg.HTTP.Routers, "dash", "router pointing at another provider must survive")
	assert.Equal(t, "api@internal", cfg.HTTP.Routers["dash"].Service)
	assert.NotContains(t, cfg.HTTP.Services, "api@internal")
}

func TestDockerRefsAreLocalized(t *testing.T) {
	t.Parallel()
	cfg := build(t, newContainer("app", ports{80: 8080}, map[string]string{
		"traefik.http.routers.app.rule":                 "Host(`app.local`)",
		"traefik.http.routers.app.service":              "app@docker",
		"traefik.http.routers.app.middlewares":          "auth@docker,gateway-mw@docker,other@file",
		"traefik.http.middlewares.auth.basicauth.users": "user:hash",
	}))
	r := cfg.HTTP.Routers["app"]
	require.NotNil(t, r)
	assert.Equal(t, "app", r.Service)
	assert.Equal(t, []string{"auth", "gateway-mw@docker", "other@file"}, r.Middlewares,
		"only middlewares defined on this host should lose the @docker suffix")
	assert.Equal(t, "http://10.0.0.5:8080", serverURL(t, cfg, "app"))
}

func TestContainersWithoutEnableLabelAreIgnored(t *testing.T) {
	t.Parallel()
	c := newContainer("app", ports{80: 8080}, map[string]string{"traefik.http.routers.app.rule": "Host(`a`)"})
	c.Labels["traefik.enable"] = "false"
	assert.Nil(t, build(t, c).HTTP)
}

// Tether handles configs as plain JSON and only reads these fields. If a traefik update renames
// one of them, this fails here, in the repo that tracks traefik, instead of breaking tether silently.
func TestOutputHasFieldsTetherReads(t *testing.T) {
	t.Parallel()
	data, err := BuildTraefikConfig([]container.Summary{newContainer("app", ports{80: 8080}, map[string]string{
		"traefik.http.routers.app.rule": "Host(`app.local`)",
	})}, hostIP)
	require.NoError(t, err)

	var raw struct {
		HTTP struct {
			Routers map[string]struct {
				Service string `json:"service"`
			} `json:"routers"`
			Services map[string]struct {
				LoadBalancer struct {
					Servers []struct {
						URL string `json:"url"`
					} `json:"servers"`
				} `json:"loadBalancer"`
			} `json:"services"`
		} `json:"http"`
	}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, "app", raw.HTTP.Routers["app"].Service, "tether reads http.routers.*.service")
	require.Len(
		t,
		raw.HTTP.Services["app"].LoadBalancer.Servers,
		1,
		"tether reads http.services.*.loadBalancer.servers",
	)
	assert.Equal(t, "http://10.0.0.5:8080", raw.HTTP.Services["app"].LoadBalancer.Servers[0].URL,
		"tether reads servers[].url")
	// encoding/json matches keys case insensitively, tether does not.
	for _, key := range []string{`"http"`, `"routers"`, `"service"`, `"services"`, `"loadBalancer"`, `"servers"`, `"url"`} {
		assert.Contains(t, string(data), key)
	}
}
