# Contributing Guidelines

Welcome to Kubernetes. We are excited about the prospect of you joining our [community](https://git.k8s.io/community)! The Kubernetes community abides by the CNCF [code of conduct](code-of-conduct.md). Here is an excerpt:

_As contributors and maintainers of this project, and in the interest of fostering an open and welcoming community, we pledge to respect all people who contribute through reporting issues, posting feature requests, updating documentation, submitting pull requests or patches, and other activities._

## Getting Started

We have full documentation on how to get started contributing here:

<!---
If your repo has certain guidelines for contribution, put them here ahead of the general k8s resources
-->

- [Contributor License Agreement](https://git.k8s.io/community/CLA.md) - Kubernetes projects require that you sign a Contributor License Agreement (CLA) before we can accept your pull requests
- [Kubernetes Contributor Guide](https://k8s.dev/guide) - Main contributor documentation, or you can just jump directly to the [contributing page](https://k8s.dev/docs/guide/contributing/)
- [Contributor Cheat Sheet](https://k8s.dev/cheatsheet) - Common resources for existing developers

## Debugging the Controller in a Cluster

You can debug the controller running inside a Kubernetes cluster using [Delve](https://github.com/go-delve/delve).

### Build and Deploy

```bash
make docker-build-debug IMG=<your-registry/your-image:tag>
make docker-push IMG=<your-registry/your-image:tag>
make deploy-debug IMG=<your-registry/your-image:tag>
```

### Connect Your Debugger

Forward the Delve port to your local machine:

```bash
kubectl port-forward -n mcp-lifecycle-operator-system deploy/mcp-lifecycle-operator-controller-manager 40000:40000
```

Then connect your IDE's remote debugger to `localhost:40000`.

**Path mapping:** Configure your IDE to map your local source root to `/workspace` inside the container.

- **GoLand:** Run > Edit Configurations > Go Remote > Path mappings: local project root → `/workspace`
- **VS Code:** In `launch.json`, set `"substitutePath"` with `"from"` as your local project root and `"to"` as `"/workspace"`

## Mentorship

- [Mentoring Initiatives](https://k8s.dev/community/mentoring) - We have a diverse set of mentorship programs available that are always looking for volunteers!

## Contact Information

- [Slack channel](https://kubernetes.slack.com/messages/sig-apps)
- [Mailing List](https://groups.google.com/a/kubernetes.io/g/sig-apps)
