# Contributing to cert-manager External Issuer

Thank you for your interest in contributing! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)

---

## Code of Conduct

This project follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). Please be respectful and inclusive in all interactions.

---

## Getting Started

### Prerequisites

- Go 1.22 or later
- Docker
- kubectl configured with access to a Kubernetes cluster
- cert-manager v1.12+ installed on the cluster
- Make

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork:
   ```bash
   git clone https://github.com/<your-username>/cert-manager-external-issuer.git
   cd cert-manager-external-issuer
   ```
3. Add the upstream remote:
   ```bash
   git remote add upstream https://github.com/example-org/cert-manager-external-issuer.git
   ```

---

## Development Setup

### Install Dependencies

```bash
# Download Go modules
go mod download

# Verify everything compiles
make build
```

### Run Tests

```bash
# Unit tests
make test

# With coverage report
make test-coverage
```

### Run Locally

You can run the controller locally against a Kubernetes cluster:

```bash
# Make sure you have a valid kubeconfig
export KUBECONFIG=~/.kube/config

# Install CRDs first
make install-crds

# Run the controller locally
make run
```

### Build and Deploy

```bash
# Build Docker image
make docker-build IMG=external-issuer:dev

# Deploy to cluster (uses kind load for kind clusters)
kind load docker-image external-issuer:dev

# Or push to a registry
make docker-push IMG=myregistry/external-issuer:dev

# Deploy
make deploy IMG=myregistry/external-issuer:dev
```

---

## Making Changes

### Branch Strategy

- Create feature branches from `main`
- Use descriptive branch names: `feature/add-vault-support`, `fix/certificate-renewal-bug`

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>

<body>

<footer>
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `style`: Code style changes (formatting, semicolons, etc.)
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(signer): add support for HashiCorp Vault PKI

Implements a new signer type that integrates with Vault's PKI secrets engine.
Supports both token and Kubernetes auth methods.

Closes #42
```

```
fix(controller): handle nil pointer in certificate request

The controller was crashing when processing a CertificateRequest with
a nil IssuerRef. Added proper nil checks and error handling.

Fixes #38
```

### Testing Your Changes

1. **Unit Tests**: Add/update unit tests for your changes
   ```bash
   make test
   ```

2. **Integration Tests**: Test against a real cluster
   ```bash
   # Deploy your changes
   make deploy IMG=your-image:tag
   
   # Create a test certificate
   kubectl apply -f examples/basic-certificate.yaml
   
   # Verify it was issued
   kubectl get certificate -A
   ```

3. **Lint**: Ensure code passes linting
   ```bash
   make lint
   ```

---

## Pull Request Process

### Before Submitting

- [ ] Code compiles without errors: `make build`
- [ ] Tests pass: `make test`
- [ ] Linting passes: `make lint`
- [ ] Documentation updated if needed
- [ ] Commit messages follow conventions
- [ ] Branch is rebased on latest `main`

### Submitting a PR

1. Push your branch to your fork
2. Open a Pull Request against `main`
3. Fill in the PR template with:
   - Description of changes
   - Related issues
   - Testing performed
   - Breaking changes (if any)

### Review Process

1. Maintainers will review your PR
2. Address any feedback
3. Once approved, a maintainer will merge your PR

---

## Coding Standards

### Go Style

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting (run via `make fmt`)
- Add comments for exported functions, types, and packages
- Handle errors explicitly, don't ignore them

### Project Structure

```
├── api/v1alpha1/           # API types (CRDs)
├── cmd/                    # Application entry points
│   ├── controller/         # External Issuer controller
│   └── mockca/             # Standalone MockCA server
├── controllers/            # Reconciler implementations
├── internal/               # Internal packages
│   └── signer/             # Signing implementations
├── deploy/                 # Kubernetes manifests
│   ├── crds/               # Custom Resource Definitions
│   ├── rbac/               # RBAC resources
│   ├── config/             # ConfigMaps
│   └── issuer/             # Example issuers
├── docs/                   # Documentation
└── examples/               # Example usage
```

### Adding a New PKI Integration

1. Add configuration parsing in `internal/signer/signer.go`
2. Implement the signing logic
3. Add tests
4. Add documentation in `docs/CONFIGURATION.md`
5. Add an example in `examples/pki-configurations.yaml`

### Documentation

- Update relevant docs for user-facing changes
- Add code comments for complex logic
- Update README if adding major features
- Include examples where helpful

---

## Questions?

- Open a GitHub issue for bugs or feature requests
- Start a discussion for general questions
- Check existing issues and discussions first

Thank you for contributing! 🎉
