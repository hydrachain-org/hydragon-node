FROM rsantev/hydra-client:latest

WORKDIR /app

USER root

RUN apk add --no-cache bash curl

COPY ./scripts/secrets.sh .
COPY ./scripts/run.sh .

# Make scripts executable
RUN chmod +x secrets.sh run.sh

EXPOSE 9632 1478 8545

ENTRYPOINT ["./run.sh"]
