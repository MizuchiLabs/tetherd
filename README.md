# Tetherd

Tetherd is a lightweight, efficient Go agent designed to run alongside your Docker containers on distributed nodes.

## Setup

### Docker

Deploy the `tetherd` agent on each worker node where you run your Docker containers. It needs access to the Docker socket to listen for events.

```yaml
services:
  tetherd:
    image: ghcr.io/mizuchilabs/tetherd:latest
    network_mode: host # To reliably detect correct local ip
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TETHERD_SERVER=http://<TETHER_SERVER_IP>:3000
      - TETHERD_TOKEN=your-secret-token
      # - TETHERD_ENVIRONMENT=production # Optional: default is "default"
      # - TETHERD_HOST_IP=1.2.3.4 # Optional: Auto-detected if not set
      # - TETHERD_INSECURE=true # Optional: default is false
      # - TETHERD_INTERVAL=300s # Optional: default is 30s
    restart: unless-stopped
```

### Binary

Download the binary from [releases](https://github.com/mizuchilabs/tetherd/releases) and run it on your worker node.

## Example Application Container

Deploy a container on your worker node as usual. The `tetherd` agent will pick up the labels, inject the correct host IP, and push the config to the central server:

```yaml
services:
  whoami:
    image: traefik/whoami
    ports:
      - "9000:80" # Exposed to host, tetherd will auto-inject http://<HOST_IP>:9000
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.whoami.rule=Host(`whoami.yourdomain.com`)"
```

Central Traefik will automatically route `whoami.yourdomain.com` to `http://<WORKER_NODE_IP>:9000`.

## License

Apache 2.0 License - see [LICENSE](LICENSE) for details.
