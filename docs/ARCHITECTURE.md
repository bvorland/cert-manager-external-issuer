# Architecture Deep Dive

This document provides detailed architecture diagrams and explanations of how the External Issuer works within Kubernetes and AKS.

## System Overview

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              Kubernetes Cluster                                   │
│                                                                                   │
│  ┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐         │
│  │   Application    │     │   Application    │     │   Application    │         │
│  │   Namespace      │     │   Namespace      │     │   Namespace      │         │
│  │                  │     │                  │     │                  │         │
│  │ ┌──────────────┐ │     │ ┌──────────────┐ │     │ ┌──────────────┐ │         │
│  │ │ Certificate  │ │     │ │ Certificate  │ │     │ │ Certificate  │ │         │
│  │ │   Resource   │ │     │ │   Resource   │ │     │ │   Resource   │ │         │
│  │ └──────┬───────┘ │     │ └──────┬───────┘ │     │ └──────┬───────┘ │         │
│  │        │         │     │        │         │     │        │         │         │
│  │ ┌──────▼───────┐ │     │ ┌──────▼───────┐ │     │ ┌──────▼───────┐ │         │
│  │ │    Secret    │ │     │ │    Secret    │ │     │ │    Secret    │ │         │
│  │ │  (TLS cert)  │ │     │ │  (TLS cert)  │ │     │ │  (TLS cert)  │ │         │
│  │ └──────────────┘ │     │ └──────────────┘ │     │ └──────────────┘ │         │
│  └──────────────────┘     └──────────────────┘     └──────────────────┘         │
│                                                                                   │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                         cert-manager Namespace                               │ │
│  │                                                                              │ │
│  │  ┌─────────────────────────────────────────────────────────────────────┐   │ │
│  │  │                     cert-manager Controller                          │   │ │
│  │  │                                                                      │   │ │
│  │  │  ┌─────────────┐    ┌─────────────────┐    ┌──────────────────┐    │   │ │
│  │  │  │ Certificate │───▶│CertificateRequest│───▶│  Watches for     │    │   │ │
│  │  │  │  Watcher    │    │    Generator     │    │  Signed Certs    │    │   │ │
│  │  │  └─────────────┘    └────────┬────────┘    └──────────────────┘    │   │ │
│  │  │                              │                                       │   │ │
│  │  └──────────────────────────────┼───────────────────────────────────────┘   │ │
│  │                                 │                                            │ │
│  └─────────────────────────────────┼────────────────────────────────────────────┘ │
│                                    │                                               │
│                                    │ Creates CertificateRequest                    │
│                                    ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────────────┐ │
│  │                    external-issuer-system Namespace                          │ │
│  │                                                                              │ │
│  │  ┌──────────────────────────────────────────────────────────────────────┐  │ │
│  │  │                   External Issuer Controller                          │  │ │
│  │  │                                                                       │  │ │
│  │  │  ┌───────────────────┐    ┌───────────────────┐    ┌──────────────┐ │  │ │
│  │  │  │  CertificateRequest│    │    PKI Signer     │    │   Status     │ │  │ │
│  │  │  │    Reconciler     │───▶│   (or MockCA)     │───▶│   Updater    │ │  │ │
│  │  │  └───────────────────┘    └─────────┬─────────┘    └──────────────┘ │  │ │
│  │  │                                     │                                │  │ │
│  │  └─────────────────────────────────────┼────────────────────────────────┘  │ │
│  │                                        │                                    │ │
│  │  ┌─────────────────┐    ┌──────────────▼──────────────┐                    │ │
│  │  │  ExternalIssuer │    │        ConfigMap            │                    │ │
│  │  │ ClusterIssuer   │    │   (PKI API configuration)   │                    │ │
│  │  └─────────────────┘    └─────────────────────────────┘                    │ │
│  │                                                                              │ │
│  └──────────────────────────────────────────────────────────────────────────────┘ │
│                                                                                    │
└───────────────────────────────────────────────────────────┬────────────────────────┘
                                                            │
                                                            │ HTTPS
                                                            ▼
                                              ┌─────────────────────────┐
                                              │    External PKI API     │
                                              │                         │
                                              │  • HashiCorp Vault      │
                                              │  • EJBCA                │
                                              │  • Venafi               │
                                              │  • Custom PKI           │
                                              └─────────────────────────┘
```

## Component Interactions

### Certificate Request Flow

```
┌──────────┐     ┌──────────────┐     ┌────────────────────┐     ┌────────────────┐
│   User   │     │ cert-manager │     │  External Issuer   │     │   PKI API      │
└────┬─────┘     └──────┬───────┘     └─────────┬──────────┘     └───────┬────────┘
     │                  │                       │                        │
     │ 1. Create        │                       │                        │
     │    Certificate   │                       │                        │
     ├─────────────────▶│                       │                        │
     │                  │                       │                        │
     │                  │ 2. Generate private   │                        │
     │                  │    key and CSR        │                        │
     │                  │◀──────────────────────│                        │
     │                  │                       │                        │
     │                  │ 3. Create             │                        │
     │                  │    CertificateRequest │                        │
     │                  ├──────────────────────▶│                        │
     │                  │                       │                        │
     │                  │                       │ 4. Read ConfigMap     │
     │                  │                       │    for PKI settings    │
     │                  │                       │◀──────────────────────│
     │                  │                       │                        │
     │                  │                       │ 5. POST CSR to PKI    │
     │                  │                       ├───────────────────────▶│
     │                  │                       │                        │
     │                  │                       │ 6. Return signed cert  │
     │                  │                       │◀───────────────────────│
     │                  │                       │                        │
     │                  │ 7. Update CR status   │                        │
     │                  │    with signed cert   │                        │
     │                  │◀──────────────────────│                        │
     │                  │                       │                        │
     │                  │ 8. Create/Update      │                        │
     │                  │    TLS Secret         │                        │
     │                  │◀──────────────────────│                        │
     │                  │                       │                        │
     │ 9. Certificate   │                       │                        │
     │    Ready!        │                       │                        │
     │◀─────────────────│                       │                        │
     │                  │                       │                        │
```

## Kubernetes API Resources

### Custom Resource Definitions (CRDs)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           external-issuer.io API Group                       │
│                                                                              │
│  ┌─────────────────────────────┐    ┌─────────────────────────────┐        │
│  │     ExternalClusterIssuer   │    │       ExternalIssuer        │        │
│  │        (Cluster-scoped)     │    │     (Namespace-scoped)      │        │
│  │                             │    │                             │        │
│  │  spec:                      │    │  spec:                      │        │
│  │    configRef:               │    │    configRef:               │        │
│  │      name: ...              │    │      name: ...              │        │
│  │      namespace: ...         │    │      namespace: ...         │        │
│  │    signerType: pki|mockca   │    │    signerType: pki|mockca   │        │
│  │    authSecretRef: ...       │    │    authSecretRef: ...       │        │
│  │                             │    │                             │        │
│  │  status:                    │    │  status:                    │        │
│  │    conditions:              │    │    conditions:              │        │
│  │      - type: Ready          │    │      - type: Ready          │        │
│  │        status: "True"       │    │        status: "True"       │        │
│  └─────────────────────────────┘    └─────────────────────────────┘        │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Relationship Between Resources

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                            Resource Dependencies                              │
│                                                                               │
│                        ┌─────────────────────┐                               │
│                        │     Certificate     │                               │
│                        │    (cert-manager)   │                               │
│                        └──────────┬──────────┘                               │
│                                   │                                          │
│                                   │ creates                                  │
│                                   ▼                                          │
│                        ┌─────────────────────┐                               │
│                        │  CertificateRequest │                               │
│                        │    (cert-manager)   │──────────────────────┐        │
│                        └──────────┬──────────┘                      │        │
│                                   │                                 │        │
│                                   │ references                      │        │
│                                   ▼                                 │        │
│    ┌─────────────────────────────────────────────────────────┐      │        │
│    │              ExternalClusterIssuer                       │      │        │
│    │              (external-issuer.io)                        │      │        │
│    └──────────────────────┬───────────────────────────────────┘      │        │
│                           │                                          │        │
│            ┌──────────────┴──────────────┐                          │        │
│            │                             │                          │        │
│            ▼                             ▼                          │        │
│    ┌───────────────┐            ┌───────────────┐                   │        │
│    │   ConfigMap   │            │    Secret     │                   │        │
│    │ (PKI config)  │            │ (credentials) │                   │        │
│    └───────────────┘            └───────────────┘                   │        │
│                                                                      │        │
│                          produces                                    │        │
│                             ┌────────────────────────────────────────┘        │
│                             ▼                                                 │
│                   ┌─────────────────────┐                                    │
│                   │       Secret        │                                    │
│                   │  (TLS certificate)  │                                    │
│                   │                     │                                    │
│                   │  data:              │                                    │
│                   │    tls.crt: ...     │                                    │
│                   │    tls.key: ...     │                                    │
│                   │    ca.crt: ...      │                                    │
│                   └─────────────────────┘                                    │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Controller Architecture

### Internal Components

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        External Issuer Controller                            │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                         Manager (controller-runtime)                    │ │
│  │                                                                         │ │
│  │  ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐      │ │
│  │  │  Scheme with    │   │  Health Probes  │   │  Leader Election │      │ │
│  │  │  API Types      │   │  /healthz       │   │  Coordination    │      │ │
│  │  │                 │   │  /readyz        │   │                  │      │ │
│  │  └─────────────────┘   └─────────────────┘   └─────────────────┘      │ │
│  │                                                                         │ │
│  │  ┌─────────────────────────────────────────────────────────────────┐  │ │
│  │  │                      Reconcilers                                 │  │ │
│  │  │                                                                  │  │ │
│  │  │  ┌─────────────────────┐  ┌─────────────────────┐              │  │ │
│  │  │  │ CertificateRequest  │  │   IssuerReconciler  │              │  │ │
│  │  │  │   Reconciler        │  │                     │              │  │ │
│  │  │  │                     │  │  • Validates config │              │  │ │
│  │  │  │  • Watches CRs      │  │  • Updates status   │              │  │ │
│  │  │  │  • Signs CSRs       │  │                     │              │  │ │
│  │  │  │  • Updates status   │  └─────────────────────┘              │  │ │
│  │  │  └──────────┬──────────┘                                        │  │ │
│  │  │             │                                                   │  │ │
│  │  └─────────────┼───────────────────────────────────────────────────┘  │ │
│  │                │                                                       │ │
│  └────────────────┼───────────────────────────────────────────────────────┘ │
│                   │                                                          │
│                   ▼                                                          │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                           Signer Package                                │ │
│  │                                                                         │ │
│  │  ┌─────────────────────┐       ┌─────────────────────┐                │ │
│  │  │      PKISigner      │       │    MockCASigner     │                │ │
│  │  │                     │       │                     │                │ │
│  │  │  • HTTP client      │       │  • Self-signed CA   │                │ │
│  │  │  • Template render  │       │  • In-memory keys   │                │ │
│  │  │  • Response parse   │       │  • Test mode        │                │ │
│  │  └─────────────────────┘       └─────────────────────┘                │ │
│  │                                                                         │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

## AKS-Specific Integration

### Network Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Azure Virtual Network                           │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         AKS Subnet                                    │   │
│  │                                                                       │   │
│  │  ┌───────────────────────────────────────────────────────────────┐  │   │
│  │  │                    AKS Cluster                                  │  │   │
│  │  │                                                                 │  │   │
│  │  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐│  │   │
│  │  │  │   Node 1    │  │   Node 2    │  │   Node 3                ││  │   │
│  │  │  │             │  │             │  │                         ││  │   │
│  │  │  │ ┌─────────┐ │  │ ┌─────────┐ │  │ ┌─────────────────────┐││  │   │
│  │  │  │ │External │ │  │ │cert-mgr │ │  │ │ Istio Ingress GW    │││  │   │
│  │  │  │ │ Issuer  │ │  │ │  Pod    │ │  │ │                     │││  │   │
│  │  │  │ └────┬────┘ │  │ └─────────┘ │  │ │ Reads TLS Secrets   │││  │   │
│  │  │  │      │      │  │             │  │ └─────────────────────┘││  │   │
│  │  │  └──────┼──────┘  └─────────────┘  └─────────────────────────┘│  │   │
│  │  │         │                                                       │  │   │
│  │  └─────────┼───────────────────────────────────────────────────────┘  │   │
│  │            │                                                           │   │
│  └────────────┼───────────────────────────────────────────────────────────┘   │
│               │                                                               │
│               │ Private Endpoint or VNet Integration                         │
│               ▼                                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         PKI Subnet                                    │    │
│  │                                                                       │    │
│  │  ┌─────────────────────────────────────────────────────────────────┐│    │
│  │  │                     PKI API Server                               ││    │
│  │  │                                                                  ││    │
│  │  │  • HashiCorp Vault                                               ││    │
│  │  │  • EJBCA                                                         ││    │
│  │  │  • Custom PKI                                                    ││    │
│  │  └─────────────────────────────────────────────────────────────────┘│    │
│  │                                                                       │    │
│  └───────────────────────────────────────────────────────────────────────┘    │
│                                                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

### Istio TLS Integration

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Istio TLS Flow                                    │
│                                                                              │
│                         External Client                                      │
│                              │                                               │
│                              │ HTTPS (443)                                   │
│                              ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        Azure Load Balancer                             │  │
│  │                        (Internal or Public)                            │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                              │                                               │
│                              ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                    Istio Ingress Gateway Pod                           │  │
│  │                                                                        │  │
│  │  ┌─────────────────┐                                                  │  │
│  │  │   Envoy Proxy   │◀─────── Reads TLS certs from Secret              │  │
│  │  │                 │         via SDS (Secret Discovery Service)       │  │
│  │  │  TLS Termination│                                                  │  │
│  │  └────────┬────────┘                                                  │  │
│  │           │                                                            │  │
│  └───────────┼────────────────────────────────────────────────────────────┘  │
│              │                                                               │
│              │ HTTP (internal)                                              │
│              ▼                                                               │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                        Application Service                             │  │
│  │                                                                        │  │
│  │  VirtualService routes traffic to backend pods                        │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │                    Secret (istio-system namespace)                     │  │
│  │                                                                        │  │
│  │  metadata:                                                             │  │
│  │    name: gateway-tls-secret     ◀──── Referenced in Gateway spec      │  │
│  │    namespace: istio-system                                            │  │
│  │  data:                                                                 │  │
│  │    tls.crt: <signed certificate>   ◀──── Created by External Issuer   │  │
│  │    tls.key: <private key>          ◀──── Generated by cert-manager    │  │
│  │    ca.crt: <CA chain>              ◀──── From PKI response            │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Certificate Renewal Lifecycle

```
Time ──────────────────────────────────────────────────────────────────────────▶

     Certificate Created              renewBefore Period         Expiration
           │                                │                        │
           ▼                                ▼                        ▼
     ┌─────┴─────┬──────────────────────────┴────────────────────────┴─────┐
     │           │                          │                              │
     │  Valid    │       Still Valid        │     Renewal Window           │ Expired
     │           │                          │                              │
     └───────────┴──────────────────────────┴──────────────────────────────┘
           │                                │
           │                                │ cert-manager triggers renewal
           │                                ▼
           │                     ┌─────────────────────────┐
           │                     │ 1. New CSR generated    │
           │                     │ 2. CertificateRequest   │
           │                     │    created              │
           │                     │ 3. External Issuer      │
           │                     │    signs new cert       │
           │                     │ 4. Secret updated       │
           │                     │ 5. Istio reloads cert   │
           │                     └─────────────────────────┘
           │                                │
           │                                ▼
           │                     New certificate valid
           │                     (zero downtime renewal)
           │
           ▼
     Old certificate continues
     working until renewal complete
```

## Security Model

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Security Boundaries                                │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    RBAC Permissions (Least Privilege)                │   │
│  │                                                                      │   │
│  │  External Issuer Service Account can:                               │   │
│  │  ✓ Read CertificateRequests (all namespaces)                        │   │
│  │  ✓ Update CertificateRequest status (all namespaces)                │   │
│  │  ✓ Read ConfigMaps (for PKI config)                                 │   │
│  │  ✓ Read Secrets (for PKI credentials)                               │   │
│  │  ✓ Create Events (for observability)                                │   │
│  │                                                                      │   │
│  │  External Issuer Service Account CANNOT:                            │   │
│  │  ✗ Create/Delete Secrets (cert-manager does this)                   │   │
│  │  ✗ Modify Certificates                                              │   │
│  │  ✗ Access application Secrets                                       │   │
│  │  ✗ Modify cluster resources                                         │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Pod Security (Hardened)                           │   │
│  │                                                                      │   │
│  │  • runAsNonRoot: true                                               │   │
│  │  • readOnlyRootFilesystem: true                                     │   │
│  │  • allowPrivilegeEscalation: false                                  │   │
│  │  • capabilities: drop ALL                                           │   │
│  │  • seccompProfile: RuntimeDefault                                   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Network Security                                  │   │
│  │                                                                      │   │
│  │  • All PKI API calls over HTTPS                                     │   │
│  │  • TLS certificate verification (configurable)                      │   │
│  │  • Private endpoints for PKI when possible                          │   │
│  │  • Network policies can restrict egress                             │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Secrets Management                                │   │
│  │                                                                      │   │
│  │  • PKI credentials stored in Kubernetes Secrets                     │   │
│  │  • Consider using external-secrets or sealed-secrets                │   │
│  │  • Private keys never leave the cluster                             │   │
│  │  • Only CSRs (public) sent to PKI                                   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```
