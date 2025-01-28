# Docker Compose setup

The purpose of the docker compose setup is to provide simple and easy-to-use setup.

_NB!_ Use more secure setup for validators.

## Installation

Ensure you have Docker installed and pull the image.

```
docker pull rsantev/hydragon-docker-compose:latest
```

## Run node

Add the needed secrets in the docker compose file in the `environment` section of the `hydra-node` service:
KEY
BLS_KEY
SIG
P2P_KEY

Run:

```
docker compose up
```
