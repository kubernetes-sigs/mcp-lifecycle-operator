# MCP Lifecycle Operator Website

This directory contains the source files for the MCP Lifecycle Operator documentation website.

## Directory Structure

```
site-src/
├── index.md              # Landing page
├── introduction.md       # Introduction and overview
├── operating/            # Day-2 operations (metrics, future topics)
│   └── metrics.md        # Prometheus metrics reference
├── guides/               # Getting started guides
│   ├── index.md
│   └── quickstart.md
├── reference/            # API reference documentation
│   └── index.md         # Auto-generated from Go API types
├── contributing/         # Contributing guide
│   └── index.md
├── images/               # Image assets
└── stylesheets/          # Custom CSS
    └── extra.css
```

## Building the Website

### Prerequisites

- Docker (recommended for local development)
- Python 3.11+ with pip (for local development without Docker)

### Local Development

**Option 1: Using Docker (recommended for consistency)**

```bash
make live-docs
```

This will start a development server at http://localhost:3000

**Option 2: Using Python virtual environment (faster startup)**

```bash
./hack/mkdocs/local-serve.sh
```

This will start a development server at http://127.0.0.1:3000

### Build for Production

Build the static site:

```bash
make build-docs
```

The generated site will be in the `site/` directory.

### Generate API Documentation

The API reference documentation is auto-generated from Go source code:

```bash
make api-ref-docs
```

This creates `site-src/reference/index.md` from the CRD types in `api/v1alpha1/`.

**Note**: The generated `index.md` file is not committed to git - it's automatically generated during the build process (`make build-docs`, `make live-docs`, or CI).

## Versioned Deployment

Documentation is versioned with [mike](https://github.com/jimporter/mike) and published to the `gh-pages` branch. The live site at [mcp-lifecycle-operator.sigs.k8s.io](https://mcp-lifecycle-operator.sigs.k8s.io) uses a version selector:

| Version | Source | Purpose |
| ------- | ------ | ------- |
| **latest** (default) | Git release tag | Matches [releases/latest](https://github.com/kubernetes-sigs/mcp-lifecycle-operator/releases/latest) install URLs |
| **main** | `main` branch | Development preview for in-flight API and behavior |

### How docs are published

- **Releases**: `.github/workflows/docs.yaml` deploys automatically when a GitHub release is published. The release tag becomes a version; the `latest` alias is updated and set as the default.
- **Main preview**: The same workflow deploys on every push to `main`.
- **Manual redeploy**: Use **Actions → Docs → Run workflow** to redeploy a specific tag (useful for bootstrapping or fixing a broken version).

### Netlify configuration

Netlify publishes the pre-built `gh-pages` branch (no build step). Complete these steps **in order** after the versioning PR merges:

1. **Merge** the versioning PR to `main`
2. **Bootstrap `latest`**: run **Actions → Docs → Deploy docs (manual)** with `v0.1.0` (or the current release tag)
3. **Switch Netlify** production branch to `gh-pages` (build command empty, publish `/`)

Until step 3, main-branch production Netlify builds fail by design; the last good production deploy stays live.

Ongoing Netlify settings:

1. **Production branch**: `gh-pages` (not `main`)
2. **Build command**: (empty)
3. **Publish directory**: `/`

The `netlify.toml` on `gh-pages` is maintained automatically by `hack/mkdocs/deploy.sh`.

### Local multi-version preview

To preview deployed versions locally:

```bash
pip install -r hack/mkdocs/image/requirements.txt
git fetch origin gh-pages
mike serve
```

For day-to-day editing, use `make live-docs` or `./hack/mkdocs/local-serve.sh` (single-version preview from your working tree).

## Deployment

The website is deployed to Netlify from the **`gh-pages`** branch. Source changes on `main` trigger a GitHub Actions workflow that rebuilds the `main` preview version; release tags update the `latest` version.

Netlify configuration: `netlify.toml` (on `gh-pages`; see [Versioned Deployment](#versioned-deployment))

## Technology Stack

- **Static Site Generator**: MkDocs ~1.6
- **Theme**: Material for MkDocs ~9.5
- **Versioning**: [mike](https://github.com/jimporter/mike)
- **Plugins**:
  - `awesome-pages`: Advanced navigation control
  - `mermaid2`: Diagram support
  - `search`: Full-text search

## Making Changes

1. Edit content in `site-src/`
2. Run `make live-docs` to preview changes
3. Commit and push to `main` — the Docs workflow updates the **main** preview on gh-pages; release docs update when a GitHub release is published

## Content Guidelines

- Use Markdown with Material extensions
- Include code examples where appropriate
- Add diagrams using Mermaid syntax
- Keep navigation structure in `.pages` files
