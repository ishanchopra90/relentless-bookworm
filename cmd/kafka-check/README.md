# Kafka Connectivity Check

This tool validates Kafka connectivity from your local machine.

## Port-forward Test

In one terminal, forward the Kafka bootstrap service:

```sh
kubectl port-forward -n kafka svc/relentless-kafka-kafka-bootstrap 9092:9092
```

In another terminal, run the check:

```sh
go run cmd/kafka-check/main.go
```

You can override the broker via `KAFKA_BROKER`:

```sh
KAFKA_BROKER=localhost:9092 go run cmd/kafka-check/main.go
```
