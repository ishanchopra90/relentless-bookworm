# Graph Writer (Local Image)

Build and load the graph-writer image into kind:

```sh
docker build -f Dockerfile.graph-writer -t relentless-graph-writer:dev .
kind load docker-image relentless-graph-writer:dev --name relentless
```

Deploy the graph writer:

```sh
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-deployment.yaml
kubectl apply -f deploy/kubernetes/graph-writer/graph-writer-metrics-service.yaml
```

Ensure Neo4j is deployed (required for graph writes):

```sh
kubectl apply -f deploy/kubernetes/neo4j/neo4j-deployment.yaml
kubectl apply -f deploy/kubernetes/neo4j/neo4j-service.yaml
```

Default Neo4j credentials:
- user: `neo4j`
- password: `relentless`

Apply uniqueness constraints (required to prevent duplicates):

```cypher
// run in Neo4j Browser
CREATE CONSTRAINT book_key_unique IF NOT EXISTS
FOR (b:Book)
REQUIRE b.key IS UNIQUE;

CREATE CONSTRAINT work_key_unique IF NOT EXISTS
FOR (w:Work)
REQUIRE w.key IS UNIQUE;

CREATE CONSTRAINT author_key_unique IF NOT EXISTS
FOR (a:Author)
REQUIRE a.key IS UNIQUE;
```

View the graph in Neo4j Browser:

```sh
kubectl port-forward svc/relentless-neo4j 7474:7474 7687:7687
```

Then open `http://localhost:7474`, run `:server connect`,
and connect to `bolt://localhost:7687`.

Verify the pod:

```sh
kubectl wait --for=condition=Available deploy/relentless-graph-writer --timeout=300s
```

Check graph-writer logs:

```sh
kubectl logs deploy/relentless-graph-writer
```

Metrics endpoint:

```sh
kubectl port-forward svc/relentless-graph-writer 9091:9091
curl http://localhost:9091/metrics
```
