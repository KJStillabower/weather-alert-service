# Sample Containers

Build `weather_service` and `weather_memcache` containers for deployment.

## Build

From project root:

```bash
./samples/containers/build_containers.sh
```

Or with a custom tag:

```bash
./samples/containers/build_containers.sh v1.0
```

## Images

| Image | Description |
|-------|-------------|
| `weather_service` | Go service; ENV_NAME=prod, WEATHER_API_KEY required |
| `weather_memcache` | Memcached; port 11211 |

## Run Locally

```bash
# Start memcached
docker run -d -p 11211:11211 --name weather-memcache weather_memcache:latest

# Start service (set WEATHER_API_KEY)
docker run -d -p 8080:8080 \
  -e WEATHER_API_KEY=your_key \
  -e MEMCACHED_ADDRS=host.docker.internal:11211 \
  weather_service:latest
```

For Linux, use the host's IP or `--add-host=host.docker.internal:host-gateway` instead of `host.docker.internal` if your Docker version supports it.

## Kubernetes

See `samples/containers/k8s/` for sample manifests: namespace, weather-memcache (Deployment + Service), weather-service (Deployment + Service). Create the `weather-api-key` secret before deploying.
