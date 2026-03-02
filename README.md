# kradar

`kradar` is a friendly command-line and terminal dashboard tool for **keeping an eye on Helm releases in Kubernetes**.

It helps you quickly answer questions like:
- *Which releases are installed across my clusters/namespaces?*
- *Are my installed chart versions up to date with their repositories?*
- *How many pods are running for each release, and what images are in use?*

If this tool saves you time (or frustration), that’s awesome — and we’d love your feedback, ideas, and contributions to make it even better for everyone. 🤝

## Why kradar

Operating multiple Helm releases across environments can get noisy fast. `kradar` gives you a single, practical view of:
- Release inventory
- Chart freshness (`up_to_date`, `outdated`, `unknown`)
- Pod rollout counts
- Image inventory

You can use it in:
- **CLI mode** for scripts and CI checks
- **TUI mode** for an interactive dashboard experience

## Features

- Scan Helm-installed workloads in one namespace or all namespaces
- Compare installed chart versions against Helm repository indexes
- Render output as table or JSON
- Inspect a single release in detail
- Test chart repository connectivity/parsing (`repos test`)
- Optional non-zero exit on outdated releases (`--fail-on outdated`) for CI gates
- Built-in TUI with refresh support

## Installation

### Build from source

```bash
go build -o kradar .
```

### Run without building

```bash
go run . --help
```

## Quick Start

### 1) Create config from example

```bash
mkdir -p ~/.config/kradar
cp configs/config.example.yaml ~/.config/kradar/config.yaml
```

### 2) List releases

```bash
kradar list --all-namespaces
```

### 3) Launch interactive dashboard

```bash
kradar tui --refresh 15s
```

### 4) Inspect one release

```bash
kradar inspect <release-name> --namespace <namespace>
```

## Configuration

By default, `kradar` loads config from:

- Linux/macOS: `$XDG_CONFIG_HOME/kradar/config.yaml` (or platform equivalent)

Override with:

```bash
kradar --config /path/to/config.yaml list
```

### Example: add a private Nexus Helm source

Use environment-variable-based auth in your config:

```yaml
chart_sources:
  - name: nexus-internal
    type: helm_index
    url: https://nexus.company.local/repository/helm-hosted
    charts: ["*"]
    priority: 100
    auth:
      type: basic_env
      username_env: NEXUS_USERNAME
      password_env: NEXUS_PASSWORD
```

Then export credentials before running `kradar`.

## Common Commands

```bash
# List all releases
kradar list --all-namespaces

# JSON output (great for automation)
kradar list --output json

# Fail CI if anything is outdated
kradar list --fail-on outdated

# Validate configured chart repositories
kradar repos test

# Show version metadata
kradar version
```

## Development

```bash
make fmt
make lint
make test
make check
```

## Contributing

Contributions are very welcome. If you have ideas, bug fixes, UX improvements, or docs updates, please open an issue or PR.

- Start here: [CONTRIBUTING.md](CONTRIBUTING.md)
- Please review: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- Security policy: [SECURITY.md](SECURITY.md)

Even small improvements help a lot. If you find `kradar` useful, collaborating on it would be truly awesome. 🙌

## License

[MIT](LICENSE)
