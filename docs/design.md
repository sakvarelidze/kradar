# Design Overview

The CLI models each Helm release as a service row.

Packages:

- `internal/helm`: release discovery and metadata decoding.
- `internal/kube`: Kubernetes API access, pod counting, and image extraction.
- `internal/check`: chart freshness checks through Helm repo `index.yaml`.
- `internal/cache`: in-memory HTTP cache with TTL.
- `internal/output`: table/json rendering.

MVP scope includes chart checks only (`--check helmrepo`). Image and GitHub checks are currently scaffolded as flags for future adapters.
