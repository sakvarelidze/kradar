# svc CLI

## Commands

- `svc list` (default namespace)
- `svc list --all-namespaces`
- `svc list --namespace <ns>`
- `svc inspect <release> --namespace <ns>`

## Output

`--output table|json`

Table columns:

`NAMESPACE | RELEASE | CHART@VER | APPVER | PODS | CHART_STATUS | IMAGES`

## Chart freshness logic

1. Discover Helm release metadata from Helm-owned Secrets/ConfigMaps.
2. Resolve chart repo URL via config mappings.
3. Download `index.yaml` and compare installed chart version with latest semver entry.
4. Return status `up_to_date`, `outdated`, or `unknown`.

## Notes

- Image and GitHub freshness checks are behind flags and default to off.
- OCI Helm charts are currently marked `unknown` unless explicit resolver support is added.
