# Multi-seed load generator

Reads seeds from a JSON config file and submits them to the API service **simultaneously** (one goroutine per seed).

## Config format

JSON file with a `seeds` array. Each entry is a seed URL or search query (same as the API `url` query param):

```json
{
  "seeds": [
    "https://openlibrary.org/works/OL45883W",
    "https://openlibrary.org/authors/OL9388A",
    "shakespeare"
  ]
}
```

## Usage

```sh
# Default: config=seeds.json, api=http://localhost:30080 (Kind nodePort)
go run ./cmd/loadgen

# Custom config and API base
go run ./cmd/loadgen -config=my_seeds.json -api=http://relentless-api:8080
```

Flags:

- `-config` — Path to JSON config file (default: `seeds.json`)
- `-api` — API base URL (default: `http://localhost:30080`, matches Kind nodePort when running loadgen on host)

Ensure the API is running and reachable before running the loadgen.
