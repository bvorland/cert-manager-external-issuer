# How It Works: Deep Dive into AKS Integration

This document provides a detailed technical explanation of how the External PKI Issuer works inside an AKS (Azure Kubernetes Service) cluster.

## Table of Contents

1. [Component Overview](#component-overview)
2. [The Certificate Lifecycle](#the-certificate-lifecycle)
3. [Controller Internals](#controller-internals)
4. [Kubernetes API Interactions](#kubernetes-api-interactions)
5. [PKI Signing Flow](#pki-signing-flow)
6. [Integration with Istio](#integration-with-istio)

---

## Component Overview

### What Gets Deployed

When you install the External PKI Issuer, the following components are created:

```plaintext
┌─────────────────────────────────────────────────────────────────────────────┐
│                           AKS Cluster                                        │
│                                                                              │
│  Namespace: external-issuer-system                                          │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                                                                         │ │
│  │  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────────┐   │ │
│  │  │   Deployment     │  │    ConfigMap     │  │      Secret        │   │ │
│  │  │   controller     │  │    pki-config    │  │      pki-auth      │   │ │
│  │  │                  │  │                  │  │                    │   │ │
│  │  │  • 2 replicas    │  │  • PKI API URL   │  │  • API token       │   │ │
│  │  │  • Leader elect  │  │  • HTTP method   │  │  • Client cert     │   │ │
│  │  │  • Health probes │  │  • Parameters    │  │  • Password        │   │ │
│  │  └────────┬─────────┘  └────────┬─────────┘  └──────────┬─────────┘   │ │
│  │           │                     │                       │              │ │
│  │           └─────────────────────┴───────────────────────┘              │ │
│  │                                 │                                       │ │
│  │                                 ▼                                       │ │
│  │           ┌───────────────────────────────────────────┐                │ │
│  │           │          ServiceAccount                   │                │ │
│  │           │          external-issuer-controller       │                │ │
│  │           └───────────────────────────────────────────┘                │ │
│  │                                 │                                       │ │
│  └─────────────────────────────────┼───────────────────────────────────────┘ │
│                                    │                                         │
│  Cluster-Scoped Resources          ▼                                         │
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │  ┌──────────────────────┐  ┌────────────────────────────────────────┐  │ │
│  │  │     ClusterRole      │  │     ClusterRoleBinding                 │  │ │
│  │  │                      │  │                                        │  │ │
│  │  │  • certificaterequests│  │  Binds ClusterRole to ServiceAccount │  │ │
│  │  │  • clusterissuers    │  │                                        │  │ │
│  │  │  • secrets           │  │                                        │  │ │
│  │  │  • configmaps        │  │                                        │  │ │
│  │  └──────────────────────┘  └────────────────────────────────────────┘  │ │
│  │                                                                         │ │
│  │  ┌──────────────────────────────────────────────────────────────────┐  │ │
│  │  │              Custom Resource Definitions (CRDs)                   │  │ │
│  │  │                                                                   │  │ │
│  │  │  • externalissuers.external-issuer.io (namespaced)               │  │ │
│  │  │  • externalclusterissuers.external-issuer.io (cluster-scoped)    │  │ │
│  │  └──────────────────────────────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘
```

### How Components Interact

```plaintext
User Request                    cert-manager                External Issuer
     │                              │                            │
     │  1. Create Certificate       │                            │
     │─────────────────────────────▶│                            │
     │                              │                            │
     │                              │  2. Generate private key   │
     │                              │     Create CSR             │
     │                              │     Create CertificateRequest
     │                              │────────────────────────────▶
     │                              │                            │
     │                              │                            │  3. Watch event
     │                              │                            │     received
     │                              │                            │
     │                              │                            │  4. Validate issuer
     │                              │                            │     Load PKI config
     │                              │                            │
     │                              │                            │  5. Call external
     │                              │                            │     PKI API
     │                              │                            │        │
     │                              │                            │        ▼
     │                              │                            │   ┌─────────┐
     │                              │                            │   │ PKI API │
     │                              │                            │   └────┬────┘
     │                              │                            │        │
     │                              │                            │◀───────┘
     │                              │                            │  Signed cert
     │                              │                            │
     │                              │  6. Update CertificateRequest
     │                              │◀────────────────────────────
     │                              │     status.certificate = PEM
     │                              │                            │
     │                              │  7. Create/Update Secret   │
     │                              │                            │
     │  8. Secret ready             │                            │
     │◀─────────────────────────────│                            │
```

---

## The Certificate Lifecycle

### Stage 1: Certificate Request

When a user creates a `Certificate` resource:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-app-tls
  namespace: production
spec:
  secretName: my-app-tls
  duration: 2160h      # 90 days
  renewBefore: 360h    # Renew 15 days before expiry
  dnsNames:
    - my-app.example.com
    - www.my-app.example.com
  issuerRef:
    name: pki-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

**What happens in AKS:**

1. kubectl/API server validates the Certificate resource
2. cert-manager's certificate controller sees the new Certificate
3. cert-manager generates a 2048-bit RSA private key
4. cert-manager creates a CSR (Certificate Signing Request) containing:
   - Subject information
   - DNS SANs from `dnsNames`
   - Key usage extensions

### Stage 2: CertificateRequest Creation

cert-manager creates a `CertificateRequest` resource:

```yaml
apiVersion: cert-manager.io/v1
kind: CertificateRequest
metadata:
  name: my-app-tls-xxxxx
  namespace: production
  ownerReferences:
    - apiVersion: cert-manager.io/v1
      kind: Certificate
      name: my-app-tls
spec:
  issuerRef:
    name: pki-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
  request: |
    -----BEGIN CERTIFICATE REQUEST-----
    MIICvDCCAaQCAQAwdzELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNV
    ...base64 encoded CSR...
    -----END CERTIFICATE REQUEST-----
  usages:
    - digital signature
    - key encipherment
    - server auth
status:
  conditions:
    - type: Approved
      status: "True"
      reason: "cert-manager.io"
```

### Stage 3: External Issuer Processing

The External Issuer controller:

1. **Watches** for `CertificateRequest` resources via the Kubernetes API
2. **Filters** to only process requests where `issuerRef.group == "external-issuer.io"`
3. **Validates** the request is approved and not already processed
4. **Loads** configuration from ConfigMap and credentials from Secret
5. **Calls** the external PKI API
6. **Updates** the CertificateRequest status with the signed certificate

### Stage 4: Secret Creation

Once the CertificateRequest is fulfilled, cert-manager:

1. Reads the signed certificate from `CertificateRequest.status.certificate`
2. Creates/updates the target Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-tls
  namespace: production
  annotations:
    cert-manager.io/certificate-name: my-app-tls
    cert-manager.io/issuer-kind: ExternalClusterIssuer
    cert-manager.io/issuer-name: pki-cluster-issuer
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-certificate-chain>
  tls.key: <base64-encoded-private-key>
  ca.crt: <base64-encoded-ca-certificate>
```

### Stage 5: Automatic Renewal

cert-manager monitors the certificate expiration:

```plaintext
Timeline:
├── Day 0: Certificate issued (90-day validity)
│
├── Day 75: renewBefore threshold (15 days before expiry)
│           cert-manager creates new CertificateRequest
│           External Issuer signs new certificate
│           Secret updated with new certificate
│           Private key optionally rotated
│
└── Day 90: Original certificate would expire
            (but was already renewed on Day 75)
```

---

## Controller Internals

### Reconciliation Loop

The controller uses the Kubernetes controller-runtime library:

```go
func (r *CertificateRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch the CertificateRequest
    cr := &cmapi.CertificateRequest{}
    if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Check if this request is for our issuer type
    if cr.Spec.IssuerRef.Group != "external-issuer.io" {
        return ctrl.Result{}, nil  // Not our responsibility
    }

    // 3. Skip if already processed
    if len(cr.Status.Certificate) > 0 || isInTerminalState(cr) {
        return ctrl.Result{}, nil
    }

    // 4. Check if approved
    if !isCertificateRequestApproved(cr) {
        return ctrl.Result{}, nil  // Wait for approval
    }

    // 5. Get issuer configuration
    issuerSpec, err := r.getIssuerSpec(ctx, cr)
    if err != nil {
        return ctrl.Result{}, r.setStatus(ctx, cr, "IssuerNotFound", err.Error())
    }

    // 6. Load PKI configuration from ConfigMap
    pkiConfig, err := r.loadPKIConfig(ctx, issuerSpec.ConfigMapRef)
    if err != nil {
        return ctrl.Result{}, r.setStatus(ctx, cr, "ConfigError", err.Error())
    }

    // 7. Create signer and sign the CSR
    signer := NewPKISigner(pkiConfig)
    certPEM, caPEM, err := signer.Sign(cr.Spec.Request, 365)
    if err != nil {
        return ctrl.Result{}, r.setStatus(ctx, cr, "SigningFailed", err.Error())
    }

    // 8. Update CertificateRequest with signed certificate
    cr.Status.Certificate = certPEM
    cr.Status.CA = caPEM
    return ctrl.Result{}, r.setStatus(ctx, cr, "Issued", "Certificate issued successfully")
}
```

### Controller Manager Configuration

```go
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    Scheme:                 scheme,
    MetricsBindAddress:     ":8080",
    HealthProbeBindAddress: ":8081",
    LeaderElection:         true,
    LeaderElectionID:       "external-issuer.io",
})
```

**Key behaviors:**

- **Leader Election**: Only one controller instance processes requests at a time
- **Health Probes**: `/healthz` and `/readyz` endpoints for Kubernetes
- **Metrics**: Prometheus metrics exposed on `:8080`
- **Watch Filtering**: Only processes relevant CertificateRequests

---

## Kubernetes API Interactions

### RBAC Requirements

The controller needs these permissions:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: external-issuer-controller
rules:
  # Watch and update CertificateRequests
  - apiGroups: ["cert-manager.io"]
    resources: ["certificaterequests"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["cert-manager.io"]
    resources: ["certificaterequests/status"]
    verbs: ["get", "update", "patch"]
  
  # Read our issuer types
  - apiGroups: ["external-issuer.io"]
    resources: ["externalissuers", "externalclusterissuers"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["external-issuer.io"]
    resources: ["externalissuers/status", "externalclusterissuers/status"]
    verbs: ["get", "update", "patch"]
  
  # Read configuration
  - apiGroups: [""]
    resources: ["configmaps", "secrets"]
    verbs: ["get", "list", "watch"]
  
  # Create events for observability
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
```

### Watch Mechanism

The controller establishes persistent watches on the Kubernetes API:

```plaintext
┌────────────────────┐         Watch Connection          ┌─────────────────┐
│                    │◀──────────────────────────────────│                 │
│  Kubernetes API    │                                   │    Controller   │
│     Server         │  Event: CertificateRequest added  │                 │
│                    │──────────────────────────────────▶│   Reconcile()   │
│                    │                                   │                 │
│                    │  PATCH: Update CR status          │                 │
│                    │◀──────────────────────────────────│                 │
└────────────────────┘                                   └─────────────────┘
```

---

## PKI Signing Flow

### Step-by-Step Signing Process

```plaintext
┌──────────────────────────────────────────────────────────────────────────────┐
│                          External Issuer Controller                           │
│                                                                               │
│  1. Receive CertificateRequest with CSR                                      │
│     ┌──────────────────────────────────────────────────────────────────────┐ │
│     │  -----BEGIN CERTIFICATE REQUEST-----                                 │ │
│     │  MIICvDCCAaQCAQAwdzELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3Q...         │ │
│     │  -----END CERTIFICATE REQUEST-----                                   │ │
│     └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                          │
│                                    ▼                                          │
│  2. Parse CSR to extract subject and SANs                                    │
│     ┌──────────────────────────────────────────────────────────────────────┐ │
│     │  Subject: CN=my-app.example.com, O=MyOrg                             │ │
│     │  DNS SANs: my-app.example.com, www.my-app.example.com                │ │
│     │  Key Usage: digitalSignature, keyEncipherment                        │ │
│     └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                          │
│                                    ▼                                          │
│  3. Build HTTP request based on ConfigMap                                    │
│     ┌──────────────────────────────────────────────────────────────────────┐ │
│     │  POST https://pki.example.com/api/sign                               │ │
│     │  Authorization: Bearer <token-from-secret>                           │ │
│     │  Content-Type: application/x-www-form-urlencoded                     │ │
│     │                                                                       │ │
│     │  action=new&subject=CN=my-app.example.com,O=MyOrg&                   │ │
│     │  san_dns1=my-app.example.com&san_dns2=www.my-app.example.com         │ │
│     └──────────────────────────────────────────────────────────────────────┘ │
│                                    │                                          │
│                                    ▼                                          │
└────────────────────────────────────┼──────────────────────────────────────────┘
                                     │
                    ┌────────────────┴────────────────┐
                    │       Network (HTTPS)           │
                    │   (May go through Azure FW)     │
                    └────────────────┬────────────────┘
                                     │
                                     ▼
┌──────────────────────────────────────────────────────────────────────────────┐
│                         External PKI API                                      │
│                                                                               │
│  4. PKI validates request and signs certificate                              │
│     - Checks authorization                                                   │
│     - Validates subject against policy                                       │
│     - Signs certificate with CA private key                                  │
│     - Returns certificate chain                                              │
│                                                                               │
│  5. Response                                                                 │
│     ┌──────────────────────────────────────────────────────────────────────┐ │
│     │  -----BEGIN CERTIFICATE-----                                         │ │
│     │  MIID... (leaf certificate)                                          │ │
│     │  -----END CERTIFICATE-----                                           │ │
│     │  -----BEGIN CERTIFICATE-----                                         │ │
│     │  MIIE... (intermediate CA)                                           │ │
│     │  -----END CERTIFICATE-----                                           │ │
│     │  -----BEGIN CERTIFICATE-----                                         │ │
│     │  MIIF... (root CA)                                                   │ │
│     │  -----END CERTIFICATE-----                                           │ │
│     └──────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## Integration with Istio

When used with Istio service mesh, the certificates enable TLS at the ingress gateway:

```plaintext
┌─────────────────────────────────────────────────────────────────────────────┐
│                              AKS Cluster                                     │
│                                                                              │
│   External Traffic                                                           │
│        │                                                                     │
│        ▼                                                                     │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                    Azure Load Balancer                               │   │
│   │                    (Internal or External)                            │   │
│   └────────────────────────────┬────────────────────────────────────────┘   │
│                                │                                             │
│                                ▼                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                   Istio Ingress Gateway                              │   │
│   │                                                                      │   │
│   │   Gateway resource:                                                  │   │
│   │   ┌───────────────────────────────────────────────────────────────┐ │   │
│   │   │ spec:                                                          │ │   │
│   │   │   servers:                                                     │ │   │
│   │   │   - port: 443                                                  │ │   │
│   │   │     tls:                                                       │ │   │
│   │   │       mode: SIMPLE                                             │ │   │
│   │   │       credentialName: my-app-tls  ◀─── References Secret      │ │   │
│   │   └───────────────────────────────────────────────────────────────┘ │   │
│   │                                                                      │   │
│   │   Istio reads Secret using SDS (Secret Discovery Service)           │   │
│   │   ┌──────────────────────────────────────────────────────────────┐  │   │
│   │   │  Secret: my-app-tls (namespace: istio-system)                │  │   │
│   │   │  - tls.crt: (certificate from External Issuer)               │  │   │
│   │   │  - tls.key: (private key from cert-manager)                  │  │   │
│   │   │  - ca.crt: (CA chain from External Issuer)                   │  │   │
│   │   └──────────────────────────────────────────────────────────────┘  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                │                                             │
│                                │ TLS Terminated                              │
│                                │ mTLS to backend                             │
│                                ▼                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                      Application Pod                                 │   │
│   │   ┌───────────────────┐  ┌───────────────────────────────────────┐  │   │
│   │   │   Istio Sidecar   │  │         Application Container         │  │   │
│   │   │   (Envoy Proxy)   │◀─┤                                       │  │   │
│   │   │                   │  │                                       │  │   │
│   │   │   Handles mTLS    │  │                                       │  │   │
│   │   └───────────────────┘  └───────────────────────────────────────┘  │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Istio Certificate Flow

1. **Certificate Created**: User creates Certificate resource targeting `istio-system` namespace
2. **Secret Generated**: External Issuer signs, cert-manager creates Secret in `istio-system`
3. **SDS Discovery**: Istio's pilot watches for Secrets matching Gateway `credentialName`
4. **Hot Reload**: Envoy receives new certificate via xDS, no restart needed
5. **TLS Termination**: Ingress Gateway uses the certificate for HTTPS

### Zero-Downtime Renewal

```plaintext
Timeline:
├── T+0:    New certificate issued, Secret updated
├── T+100ms: Istio pilot detects Secret change
├── T+200ms: Pilot pushes new config to Envoy via xDS
├── T+300ms: Envoy applies new certificate
│            (existing connections continue with old cert)
│            (new connections use new cert)
└── T+5min:  All connections now using new certificate
```

---

## Summary

The External PKI Issuer extends cert-manager with these key capabilities:

1. **Declarative**: Certificates defined as Kubernetes resources
2. **Automated**: Full lifecycle management including renewal
3. **Configurable**: PKI settings in ConfigMap, easy to update
4. **Secure**: Credentials in Secrets, RBAC-controlled access
5. **Observable**: Prometheus metrics, Kubernetes events
6. **Cloud-Native**: Works with Istio, Ingress, and any TLS consumer
