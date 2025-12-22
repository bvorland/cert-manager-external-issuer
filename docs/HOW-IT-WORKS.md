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

```mermaid
flowchart TB
    subgraph cluster["AKS Cluster"]
        subgraph ns["Namespace: external-issuer-system"]
            deploy["🚀 Deployment<br/><b>controller</b><br/>• 2 replicas<br/>• Leader elect<br/>• Health probes"]
            config["📄 ConfigMap<br/><b>pki-config</b><br/>• PKI API URL<br/>• HTTP method<br/>• Parameters"]
            secret["🔐 Secret<br/><b>pki-auth</b><br/>• API token<br/>• Client cert<br/>• Password"]
            sa["👤 ServiceAccount<br/><b>external-issuer-controller</b>"]
            
            deploy --> sa
            config --> deploy
            secret --> deploy
        end
        
        subgraph cluster_scoped["Cluster-Scoped Resources"]
            cr["🔒 ClusterRole<br/>• certificaterequests<br/>• clusterissuers<br/>• secrets<br/>• configmaps"]
            crb["🔗 ClusterRoleBinding<br/>Binds ClusterRole to ServiceAccount"]
            
            subgraph crds["Custom Resource Definitions (CRDs)"]
                ei["externalissuers.external-issuer.io<br/>(namespaced)"]
                eci["externalclusterissuers.external-issuer.io<br/>(cluster-scoped)"]
            end
        end
        
        sa --> crb
        crb --> cr
    end
```

### How Components Interact

```mermaid
sequenceDiagram
    participant User
    participant CM as cert-manager
    participant EI as External Issuer
    participant PKI as PKI API
    
    User->>CM: 1. Create Certificate
    CM->>CM: 2. Generate private key & CSR
    CM->>EI: 3. Create CertificateRequest
    Note over EI: 4. Watch event received
    EI->>EI: 5. Validate issuer & load config
    EI->>PKI: 6. Call external PKI API
    PKI-->>EI: 7. Return signed certificate
    EI->>CM: 8. Update CertificateRequest status
    CM->>CM: 9. Create/Update Secret
    CM-->>User: 10. Secret ready
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

```mermaid
timeline
    title Certificate Renewal Timeline
    section Validity Period
        Day 0 : Certificate issued (90-day validity)
    section Active Period
        Day 1-74 : Certificate valid and in use
    section Renewal Window
        Day 75 : renewBefore threshold reached (15 days before expiry)
               : cert-manager creates new CertificateRequest
               : External Issuer signs new certificate
               : Secret updated with new certificate
               : Private key optionally rotated
    section Expiry
        Day 90 : Original certificate would expire
               : (but was already renewed on Day 75)
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

```mermaid
sequenceDiagram
    participant API as Kubernetes API Server
    participant Ctrl as Controller
    
    Ctrl->>API: Establish Watch Connection
    API-->>Ctrl: Event: CertificateRequest added
    Ctrl->>Ctrl: Reconcile()
    Ctrl->>API: PATCH: Update CR status
```

---

## PKI Signing Flow

### Step-by-Step Signing Process

```mermaid
flowchart TB
    subgraph controller["External Issuer Controller"]
        step1["1️⃣ Receive CertificateRequest<br/>with CSR (PEM encoded)"]
        step2["2️⃣ Parse CSR<br/>Subject: CN=my-app.example.com<br/>DNS SANs: my-app.example.com, www.my-app.example.com<br/>Key Usage: digitalSignature, keyEncipherment"]
        step3["3️⃣ Build HTTP Request<br/>POST https://pki.example.com/api/sign<br/>Authorization: Bearer token<br/>Content-Type: application/x-www-form-urlencoded"]
        
        step1 --> step2
        step2 --> step3
    end
    
    network["🌐 Network (HTTPS)<br/>May go through Azure FW"]
    
    subgraph pki["External PKI API"]
        step4["4️⃣ PKI validates & signs<br/>• Checks authorization<br/>• Validates subject against policy<br/>• Signs certificate with CA private key"]
        step5["5️⃣ Returns certificate chain<br/>• Leaf certificate<br/>• Intermediate CA<br/>• Root CA"]
        
        step4 --> step5
    end
    
    step3 --> network
    network --> step4
    step5 --> controller
```

---

## Integration with Istio

When used with Istio service mesh, the certificates enable TLS at the ingress gateway:

```mermaid
flowchart TB
    client["👤 External Client"]
    
    subgraph cluster["AKS Cluster"]
        lb["⚖️ Azure Load Balancer<br/>(Internal or External)"]
        
        subgraph istio["Istio Ingress Gateway"]
            gw["🌐 Gateway Resource<br/>spec.servers[0].tls.mode: SIMPLE<br/>credentialName: my-app-tls"]
            envoy["🔷 Envoy Proxy<br/>TLS Termination"]
        end
        
        secret["🔐 Secret: my-app-tls<br/>(istio-system namespace)<br/>• tls.crt: certificate from External Issuer<br/>• tls.key: private key from cert-manager<br/>• ca.crt: CA chain from External Issuer"]
        
        subgraph app["Application Pod"]
            sidecar["🔷 Istio Sidecar<br/>(Envoy - handles mTLS)"]
            container["📦 Application Container"]
        end
    end
    
    client -->|"HTTPS (443)"| lb
    lb --> envoy
    secret -.->|"SDS reads certs"| envoy
    gw --> envoy
    envoy -->|"HTTP (internal) / mTLS"| sidecar
    sidecar --> container
```

### Istio Certificate Flow

1. **Certificate Created**: User creates Certificate resource targeting `istio-system` namespace
2. **Secret Generated**: External Issuer signs, cert-manager creates Secret in `istio-system`
3. **SDS Discovery**: Istio's pilot watches for Secrets matching Gateway `credentialName`
4. **Hot Reload**: Envoy receives new certificate via xDS, no restart needed
5. **TLS Termination**: Ingress Gateway uses the certificate for HTTPS

### Zero-Downtime Renewal

```mermaid
timeline
    title Zero-Downtime Certificate Renewal
    section Certificate Update
        T+0 : New certificate issued, Secret updated
        T+100ms : Istio pilot detects Secret change
        T+200ms : Pilot pushes new config to Envoy via xDS
        T+300ms : Envoy applies new certificate
                : Existing connections continue with old cert
                : New connections use new cert
        T+5min : All connections now using new certificate
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
