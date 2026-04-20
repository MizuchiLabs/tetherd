# Tetherd

`tetherd` is a lightweight, efficient Go agent designed to run alongside your Docker containers on distributed nodes. It acts as an adapter for a centralized Traefik instance, allowing you to route traffic to containers on remote hosts without needing a complex orchestration setup like Docker Swarm or Kubernetes, or setting up a full KV store like Consul/Redis.

## How it works

1. Runs on your worker nodes where Docker containers are deployed.
2. Listens to local Docker events (Start, Stop, Die).
3. Parses standard Traefik labels from your containers (e.g., `traefik.enable=true`, `traefik.http.routers...`).
4. Automatically injects the correct host IP and exposed ports to ensure the central Traefik can route directly to the container's host port.
5. Exposes an in-memory JSON configuration on an HTTP endpoint for Traefik's [HTTP Provider](https://doc.traefik.io/traefik/providers/http/).

## Setup

### 1. Central Traefik Node

Configure your central Traefik instance to pull configuration from your worker nodes.

**traefik.yml:**
```yaml
providers:
  http:
    endpoints:
      - "http://<WORKER_IP_1>:8080/config"
      - "http://<WORKER_IP_2>:8080/config"
    pollInterval: "5s"
```

### 2. Worker Node

Deploy `tetherd` on each worker node either as a docker container or run the binary.

#### Docker (recommended)

Since it needs to talk to Docker, you mount the Docker socket:

```yaml
services:
  tetherd:
    image: ghcr.io/mizuchilabs/tetherd:latest
    container_name: tetherd
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    ports:
      - 3000:3000
    restart: unless-stopped
```

#### Binary

Download from [releases](https://github.com/mizuchilabs/tetherd/releases) and run:

```bash
./tetherd
```

## Example Application Container

Deploy a container on your worker node as usual. tetherd will pick up the labels and serve them to the central node:

```yaml
version: "3"
services:
  whoami:
    image: traefik/whoami
    ports:
      - "9000:80" # Exposed to host, tetherd will auto-inject http://<HOST_IP>:9000
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.whoami.rule=Host(`whoami.yourdomain.com`)"
```

Central Traefik will automatically route `whoami.yourdomain.com` to `http://<WORKER_IP>:9000`.

## License

Apache 2.0 License - see [LICENSE](LICENSE) for details

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
