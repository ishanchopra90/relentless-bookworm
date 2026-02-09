# Neo4j (Local Deployment)

Deploy Neo4j into the cluster:

```sh
kubectl apply -f deploy/kubernetes/neo4j/neo4j-deployment.yaml
kubectl apply -f deploy/kubernetes/neo4j/neo4j-service.yaml
```

Wait for readiness:

```sh
kubectl wait --for=condition=Available deploy/relentless-neo4j --timeout=300s
```

Port-forward for browser access (HTTP + Bolt):

```sh
kubectl port-forward svc/relentless-neo4j 7474:7474 7687:7687
```

Default credentials:
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

1) Open `http://localhost:7474`
2) Run `:server connect`
3) Connect to `bolt://localhost:7687` with the credentials above
4) Example query:

```cypher
MATCH p=()-[]-() RETURN p LIMIT 50;
```
