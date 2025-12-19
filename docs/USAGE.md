# Usage Guide

This guide covers common usage patterns for the External Issuer with cert-manager.

## Table of Contents

- [Creating Certificates](#creating-certificates)
- [Using with Istio](#using-with-istio)
- [Using with Ingress](#using-with-ingress)
- [Certificate Renewal](#certificate-renewal)
- [Monitoring Certificates](#monitoring-certificates)

---

## Creating Certificates

### Basic Certificate

Create a simple certificate using the ClusterIssuer:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-app-tls
  namespace: my-app
spec:
  # The Secret where the certificate will be stored
  secretName: my-app-tls-secret
  
  # Certificate validity duration
  duration: 2160h  # 90 days
  
  # Renew 30 days before expiration
  renewBefore: 720h  # 30 days
  
  # Subject fields
  commonName: my-app.example.com
  
  # Additional DNS names (SANs)
  dnsNames:
    - my-app.example.com
    - www.my-app.example.com
  
  # Reference to the issuer
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

### Wildcard Certificate

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: wildcard-tls
  namespace: istio-system
spec:
  secretName: wildcard-tls-secret
  duration: 2160h
  renewBefore: 720h
  commonName: "*.example.com"
  dnsNames:
    - "*.example.com"
    - "example.com"
  issuerRef:
    name: pki-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

### Multi-Domain Certificate

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: multi-domain-tls
  namespace: default
spec:
  secretName: multi-domain-tls-secret
  duration: 2160h
  renewBefore: 720h
  commonName: api.example.com
  dnsNames:
    - api.example.com
    - admin.example.com
    - portal.example.com
  ipAddresses:
    - 10.0.0.1
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

---

## Using with Istio

### Gateway with External Issuer Certificate

1. **Create the Certificate in istio-system:**

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: gateway-tls
  namespace: istio-system
spec:
  secretName: gateway-tls-secret
  duration: 2160h
  renewBefore: 720h
  commonName: "*.apps.example.com"
  dnsNames:
    - "*.apps.example.com"
    - "apps.example.com"
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

2. **Create the Istio Gateway:**

```yaml
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
  name: apps-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
    - port:
        number: 443
        name: https
        protocol: HTTPS
      tls:
        mode: SIMPLE
        credentialName: gateway-tls-secret  # References the cert-manager Secret
      hosts:
        - "*.apps.example.com"
    - port:
        number: 80
        name: http
        protocol: HTTP
      hosts:
        - "*.apps.example.com"
      tls:
        httpsRedirect: true  # Redirect HTTP to HTTPS
```

3. **Create VirtualService for your app:**

```yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: my-app
  namespace: my-app
spec:
  hosts:
    - "my-app.apps.example.com"
  gateways:
    - istio-system/apps-gateway
  http:
    - match:
        - uri:
            prefix: /
      route:
        - destination:
            host: my-app-service
            port:
              number: 80
```

### Per-Service mTLS with Istio

For service-to-service mTLS inside the mesh:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-service-mtls
  namespace: my-app
spec:
  secretName: my-service-mtls-secret
  duration: 720h  # 30 days (shorter for internal certs)
  renewBefore: 168h  # 7 days
  commonName: my-service.my-app.svc.cluster.local
  dnsNames:
    - my-service
    - my-service.my-app
    - my-service.my-app.svc
    - my-service.my-app.svc.cluster.local
  usages:
    - server auth
    - client auth
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

---

## Using with Ingress

### Kubernetes Ingress with cert-manager annotation

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app-ingress
  namespace: my-app
  annotations:
    cert-manager.io/cluster-issuer: mockca-cluster-issuer
    cert-manager.io/issuer-group: external-issuer.io
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - my-app.example.com
      secretName: my-app-tls-auto  # cert-manager creates this automatically
  rules:
    - host: my-app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app-service
                port:
                  number: 80
```

### Pre-created Certificate for Ingress

```yaml
# First, create the certificate
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: my-app-tls
  namespace: my-app
spec:
  secretName: my-app-tls-secret
  duration: 2160h
  renewBefore: 720h
  commonName: my-app.example.com
  dnsNames:
    - my-app.example.com
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
---
# Then reference it in the Ingress
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app-ingress
  namespace: my-app
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - my-app.example.com
      secretName: my-app-tls-secret  # Pre-created by cert-manager
  rules:
    - host: my-app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app-service
                port:
                  number: 80
```

---

## Certificate Renewal

### How Renewal Works

cert-manager automatically handles certificate renewal:

1. Monitors certificate expiration based on `renewBefore`
2. Creates a new CertificateRequest when renewal is due
3. External Issuer signs the new CSR
4. cert-manager updates the Secret with the new certificate
5. Istio/Ingress detects the Secret update and reloads

### Manual Renewal

Force immediate renewal by deleting the Secret:

```bash
# This triggers cert-manager to create a new certificate
kubectl delete secret my-app-tls-secret -n my-app
```

Or by updating the Certificate resource:

```bash
# Add or modify an annotation to trigger renewal
kubectl annotate certificate my-app-tls -n my-app \
  cert-manager.io/issuer-refresh="$(date +%s)"
```

### Checking Renewal Status

```bash
# View certificate status
kubectl get certificate my-app-tls -n my-app -o yaml

# Check CertificateRequest history
kubectl get certificaterequests -n my-app

# View certificate details from Secret
kubectl get secret my-app-tls-secret -n my-app -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -text -noout
```

---

## Monitoring Certificates

### List All Certificates

```bash
# All certificates in the cluster
kubectl get certificates -A

# With status details
kubectl get certificates -A -o wide
```

### Check Certificate Health

```bash
# Create a script to check all certificates
kubectl get certificates -A -o json | jq -r '
  .items[] | 
  "\(.metadata.namespace)/\(.metadata.name): Ready=\(.status.conditions[]? | select(.type=="Ready") | .status)"
'
```

### Prometheus Metrics

The external-issuer exposes metrics for monitoring:

```yaml
# Example PrometheusRule for certificate alerts
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: certificate-alerts
  namespace: monitoring
spec:
  groups:
    - name: certificates
      rules:
        - alert: CertificateExpiringSoon
          expr: |
            certmanager_certificate_expiration_timestamp_seconds - time() < 7 * 24 * 3600
          for: 1h
          labels:
            severity: warning
          annotations:
            summary: "Certificate {{ $labels.name }} expires in less than 7 days"
            
        - alert: CertificateNotReady
          expr: |
            certmanager_certificate_ready_status{condition="False"} == 1
          for: 15m
          labels:
            severity: critical
          annotations:
            summary: "Certificate {{ $labels.name }} is not ready"
```

### Grafana Dashboard

Import the cert-manager dashboard (ID: 11001) and add custom panels for external-issuer metrics:

- Certificate signing latency
- Signing success/failure rate
- PKI API response times

---

## Next Steps

- [Troubleshooting Guide](TROUBLESHOOTING.md) - Common issues and solutions
- [Configuration Guide](CONFIGURATION.md) - PKI API configuration options
- [How It Works](HOW-IT-WORKS.md) - Technical deep-dive
