<p align="center">
<img src="./.github/logo.svg" width="80">
<br><br>
<img alt="GitHub Tag" src="https://img.shields.io/github/v/tag/MizuchiLabs/tetherd?label=Version">
<img alt="GitHub License" src="https://img.shields.io/github/license/MizuchiLabs/tetherd">
</p>
<img alt="GitHub Issues or Pull Requests" src="https://img.shields.io/github/issues/MizuchiLabs/tetherd">

# Tetherd

**Tetherd** is the lightweight agent that runs on your worker servers. It watches your Docker containers and tells the central [Tether](https://github.com/MizuchiLabs/tether) server where they are so Traefik can find them.

## How it Works

1. You run Tetherd on every server where you have Docker containers.
2. Tetherd looks at your containers' labels (like `traefik.http.routers.myapp.rule`).
3. It automatically detects the server's IP address.
4. It sends this information to your central **Tether** server.

This allows a single Traefik instance on a different machine to route traffic to these containers seamlessly.

## Quick Start

Run Tetherd on your worker server:

```yaml
services:
  tetherd:
    image: ghcr.io/mizuchilabs/tetherd:latest
    network_mode: host # Highly recommended for IP detection
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    environment:
      - TETHERD_SERVER=http://<TETHER_SERVER_IP>:3000
      - TETHERD_TOKEN=your-secret-password
    restart: unless-stopped
```

## Deploying an App

When you deploy an app on this worker server, just add standard Traefik labels. Tetherd handles the rest:

```yaml
services:
  my-app:
    image: nginx
    ports:
      - "8080:80"
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.my-app.rule=Host(`my-app.com`)"
```

Tetherd will tell the central server: _"Send traffic for `my-app.com` to `http://<THIS_SERVER_IP>:8080`"_.

## Configuration

| Env Var               | Flag         | Default                 | Description                                     |
| --------------------- | ------------ | ----------------------- | ----------------------------------------------- |
| `TETHERD_SERVER`      | `--server`   | `http://127.0.0.1:3000` | URL of the central Tether server.               |
| `TETHERD_TOKEN`       | `--token`    |                         | **Required**: Token matching the Tether server. |
| `TETHERD_HOST_IP`     | `--host-ip`  | _(auto)_                | Manual override for this server's IP.           |
| `TETHERD_INTERVAL`    | `--interval` | `30s`                   | How often to sync with the central server.      |
| `TETHERD_ENVIRONMENT` | `--env`      | `default`               | Group servers into isolated environments.       |
| `TETHERD_DEBUG`       | `--debug`    | `false`                 | Enable detailed logging.                        |

---

**Requirement:** You need a [Tether](https://github.com/MizuchiLabs/tether) server running to collect these updates.

## License

Apache 2.0 License - see [LICENSE](LICENSE) for details
