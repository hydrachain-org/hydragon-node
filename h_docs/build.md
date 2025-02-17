# Build procedure

Information about building and using the application

## Build production version for:

1. MacOS ARM64

```
GOOS=darwin GOARCH=arm64 make build
```

2. Linux

```
GOOS=linux GOARCH=amd64 make build
```

## Move to path

1. Linux

```
sudo mv hydra /usr/local/bin
```

## Build the hydra client's docker image

1. Build node source code

```
GOOS=linux GOARCH=amd64 make build
```

2. Build node image with the commit tag

```
docker build --platform linux/amd64 -t rsantev/hydra-client:<commit-tag> -f Dockerfile.release .
```

3. Push node image to DockerHub

Use Docker Desktop or:

```
docker push rsantev/hydra-client:<commit-tag>
```

Repeat this and push it with the tag `latest`.

4. Build hydragon docker compose image

```
docker build --platform linux/amd64 -t rsantev/hydragon-docker:latest ./docker/hydra_docker_compose
```

5. Push to DockerHub

```
docker push rsantev/hydragon-docker:latest
```

### Build devnet cluster docker image

4. Build hydragon devnet cluster image

```
cd docker/hydra_devnet_cluster \
docker build --platform linux/amd64 -t rsantev/devnet-cluster:latest .
```

5. Push image to DockerHub

```
docker push rsantev/devnet-cluster:latest
```
