# MCP Lifecycle Operator Website

This directory contains the source files for the MCP Lifecycle Operator documentation website.

## Directory Structure

```
site-src/
├── index.md              # Landing page
├── introduction.md       # Introduction and overview
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

**Note**: The generated `index.md` file is not committed to git - it's automatically generated during the build process (`make build-docs`, `make live-docs`, or `make build-docs-netlify`).

## Technology Stack

- **Static Site Generator**: MkDocs ~1.6
- **Theme**: Material for MkDocs ~9.5
- **Plugins**:
  - `awesome-pages`: Advanced navigation control
  - `mermaid2`: Diagram support
  - `search`: Full-text search

## Deployment

The website is deployed to Netlify automatically when changes are pushed to the main branch.

Netlify configuration: `netlify.toml`

## Making Changes

1. Edit content in `site-src/`
2. Run `make live-docs` to preview changes
3. Commit and push to trigger deployment

## Content Guidelines

- Use Markdown with Material extensions
- Include code examples where appropriate
- Add diagrams using Mermaid syntax
- Keep navigation structure in `.pages` files
