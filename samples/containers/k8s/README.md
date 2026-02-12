# Kubernetes Stack

Sample manifests for deploying the Weather Alert Service stack (weather_service + weather_memcache).

## Prerequisites

1. Build images: `./samples/containers/build_containers.sh`
2. Load into kind/minikube (if local): `kind load docker-image weather_service:latest weather_memcache:latest`
3. Create the API key secret (required before weather-service starts)

## Deploy Order

```bash
# 1. Create namespace
kubectl apply -f samples/containers/k8s/namespace.yaml

# 2. Create secret (replace with your key)
kubectl create secret generic weather-api-key -n weather \
  --from-literal=WEATHER_API_KEY=your_openweathermap_api_key_here

# 3. Deploy memcached
kubectl apply -f samples/containers/k8s/weather-memcache-deployment.yaml
kubectl apply -f samples/containers/k8s/weather-memcache-service.yaml

# 4. Deploy service
kubectl apply -f samples/containers/k8s/weather-service-deployment.yaml
kubectl apply -f samples/containers/k8s/weather-service-service.yaml
```

Or apply all at once (secret must exist first):

```bash
kubectl apply -f samples/containers/k8s/
```

## Components

| Resource | Purpose |
|----------|---------|
| `namespace` | Isolates weather stack |
| `weather-memcache` | Deployment + Service; port 11211 |
| `weather-service` | Deployment + Service; port 8080; depends on memcache |
| `weather-api-key` | Secret for WEATHER_API_KEY |

## Access

- **LoadBalancer:** `kubectl get svc -n weather weather-service` for EXTERNAL-IP
- **Port-forward:** `kubectl port-forward -n weather svc/weather-service 8080:80`
- **Health:** `curl http://localhost:8080/health`
- **Weather:** `curl http://localhost:8080/weather/seattle`

## Local Clusters (kind/minikube)

Images are built locally. Load them into your cluster:

```bash
# kind
kind load docker-image weather_service:latest weather_memcache:latest

# minikube
eval $(minikube docker-env)
./samples/containers/build_containers.sh
```
