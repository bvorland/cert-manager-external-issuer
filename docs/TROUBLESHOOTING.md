# Troubleshooting Guide

This guide helps you diagnose and fix common issues with the External Issuer.

## Table of Contents

- [Quick Diagnostics](#quick-diagnostics)
- [Common Issues](#common-issues)
  - [Certificate Stuck in Pending](#certificate-stuck-in-pending)
  - [CertificateRequest Denied](#certificaterequest-denied)
  - [Issuer Not Ready](#issuer-not-ready)
  - [PKI API Connection Errors](#pki-api-connection-errors)
  - [TLS/SSL Errors](#tlsssl-errors)
  - [RBAC Permission Errors](#rbac-permission-errors)
- [Debugging Commands](#debugging-commands)
- [Log Analysis](#log-analysis)
- [Getting Help](#getting-help)

---

## Quick Diagnostics

Run these commands to quickly assess the system state:

```bash
# 1. Check if the controller is running
kubectl get pods -n external-issuer-system

# 2. Check issuer status
kubectl get externalclusterissuers
kubectl get externalissuers -A

# 3. Check certificate status
kubectl get certificates -A

# 4. Check certificate requests
kubectl get certificaterequests -A

# 5. View controller logs
kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer --tail=100
```

---

## Common Issues

### Certificate Stuck in Pending

**Symptoms:**
- Certificate shows `Ready: False`
- CertificateRequest remains in `Pending` state

**Diagnosis:**

```bash
# Check certificate status
kubectl describe certificate <cert-name> -n <namespace>

# Check the associated CertificateRequest
kubectl get certificaterequest -n <namespace>
kubectl describe certificaterequest <cr-name> -n <namespace>
```

**Common Causes & Solutions:**

1. **Issuer not ready:**
   ```bash
   # Check issuer status
   kubectl get externalclusterissuer <issuer-name> -o yaml
   ```
   Solution: Fix the issuer configuration (see [Issuer Not Ready](#issuer-not-ready))

2. **Wrong issuer reference:**
   ```yaml
   # Ensure the Certificate references the correct issuer
   spec:
     issuerRef:
       name: mockca-cluster-issuer      # Must match issuer name
       kind: ExternalClusterIssuer      # Must be ExternalClusterIssuer or ExternalIssuer
       group: external-issuer.io        # IMPORTANT: Must be external-issuer.io
   ```

3. **Controller not processing:**
   ```bash
   # Check controller logs for errors
   kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer | grep -i error
   ```

---

### CertificateRequest Denied

**Symptoms:**
- CertificateRequest shows `Denied: True` or `Ready: False`
- Certificate stuck in `Issuing` state

**Diagnosis:**

```bash
kubectl describe certificaterequest <name> -n <namespace>
```

Look for `status.conditions` and `status.failureTime`.

**Common Causes & Solutions:**

1. **PKI API rejected the CSR:**
   ```bash
   # Check controller logs for API response
   kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer | grep -A5 "signing request"
   ```
   - Verify the CSR format matches PKI expectations
   - Check if the common name/SANs are allowed by the PKI policy

2. **Invalid certificate template:**
   - Ensure `duration` is within PKI limits
   - Verify `dnsNames` and `ipAddresses` are properly formatted

3. **Authentication failed:**
   - Check PKI credentials in the Secret
   - Verify token/certificate hasn't expired

---

### Issuer Not Ready

**Symptoms:**
- `ExternalClusterIssuer` or `ExternalIssuer` shows `Ready: False`

**Diagnosis:**

```bash
kubectl describe externalclusterissuer <name>
```

**Common Causes & Solutions:**

1. **ConfigMap not found:**
   ```bash
   # Verify ConfigMap exists
   kubectl get configmap external-issuer-config -n external-issuer-system
   ```
   Solution: Create the ConfigMap (see [deploy/config/pki-config.yaml](../deploy/config/pki-config.yaml))

2. **Invalid ConfigMap reference:**
   ```yaml
   spec:
     configRef:
       name: external-issuer-config       # Must match ConfigMap name
       namespace: external-issuer-system  # Must match ConfigMap namespace
   ```

3. **Missing credentials Secret:**
   ```bash
   kubectl get secret pki-api-credentials -n external-issuer-system
   ```
   Solution: Create the credentials Secret

---

### PKI API Connection Errors

**Symptoms:**
- Certificate requests fail with network-related errors
- Controller logs show connection timeouts

**Diagnosis:**

```bash
# Check controller logs for network errors
kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer | grep -i "connection\|timeout\|refused"
```

**Common Causes & Solutions:**

1. **PKI not reachable from cluster:**
   ```bash
   # Test connectivity from a debug pod
   kubectl run debug --rm -it --image=curlimages/curl -- \
     curl -v https://your-pki-api.example.com/health
   ```
   - Check firewall rules
   - Verify DNS resolution
   - Check network policies

2. **Private endpoint not accessible:**
   - Ensure the AKS cluster can reach private endpoints
   - Configure Private Link if PKI is in Azure

3. **Timeout too short:**
   ```yaml
   # In ConfigMap, increase timeout
   data:
     pki-timeout: "60"  # Increase from default 30 seconds
   ```

---

### TLS/SSL Errors

**Symptoms:**
- `x509: certificate signed by unknown authority`
- `x509: certificate has expired`
- `tls: handshake failure`

**Common Causes & Solutions:**

1. **PKI uses self-signed or private CA:**
   ```yaml
   # Option A: Skip TLS verification (NOT for production!)
   data:
     pki-skip-tls-verify: "true"
   
   # Option B: Add CA certificate (recommended)
   # Mount the CA cert and reference it in the configuration
   ```

2. **PKI certificate expired:**
   - Check PKI server certificate validity
   - Contact PKI administrator

3. **Wrong TLS version:**
   - Ensure PKI API supports TLS 1.2+

---

### RBAC Permission Errors

**Symptoms:**
- `forbidden: User "system:serviceaccount:..." cannot...`
- Controller crashes with permission errors

**Diagnosis:**

```bash
# Check RBAC resources
kubectl get clusterrole external-issuer-controller-role -o yaml
kubectl get clusterrolebinding external-issuer-controller-binding -o yaml

# Verify service account
kubectl get serviceaccount external-issuer-controller -n external-issuer-system
```

**Solution:**

```bash
# Re-apply RBAC configuration
kubectl apply -f deploy/rbac/rbac.yaml
```

---

## Debugging Commands

### View Controller Logs

```bash
# Full logs
kubectl logs -n external-issuer-system deployment/external-issuer-controller

# Follow logs in real-time
kubectl logs -n external-issuer-system deployment/external-issuer-controller -f

# Show only errors
kubectl logs -n external-issuer-system deployment/external-issuer-controller | grep -i error

# Show last 100 lines with timestamps
kubectl logs -n external-issuer-system deployment/external-issuer-controller --tail=100 --timestamps
```

### Enable Debug Logging

Edit the deployment to add verbose logging:

```bash
kubectl edit deployment external-issuer-controller -n external-issuer-system
```

Add `--zap-log-level=debug` to the args:

```yaml
spec:
  containers:
  - name: controller
    args:
      - --leader-elect=true
      - --zap-log-level=debug  # Add this line
```

### Inspect Resources

```bash
# Full YAML output for a certificate
kubectl get certificate <name> -n <namespace> -o yaml

# Events related to a certificate
kubectl get events -n <namespace> --field-selector involvedObject.name=<cert-name>

# All events in the issuer namespace
kubectl get events -n external-issuer-system --sort-by='.lastTimestamp'
```

### Verify Certificate Content

```bash
# Extract and decode certificate from Secret
kubectl get secret <secret-name> -n <namespace> -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -text -noout

# Check certificate dates
kubectl get secret <secret-name> -n <namespace> -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -dates -noout

# Verify certificate chain
kubectl get secret <secret-name> -n <namespace> -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl verify -CAfile <ca-cert-file>
```

---

## Log Analysis

### Understanding Log Messages

| Log Level | Message Pattern | Meaning |
|-----------|-----------------|---------|
| INFO | `Starting controller` | Controller is initializing |
| INFO | `Reconciling CertificateRequest` | Processing a signing request |
| INFO | `Successfully signed certificate` | Certificate issued successfully |
| ERROR | `Failed to sign certificate` | Signing failed (check details) |
| ERROR | `PKI API error` | External PKI returned an error |
| WARN | `Issuer not ready` | Waiting for issuer to become ready |

### Common Error Patterns

```bash
# PKI API authentication failed
grep "401\|403\|unauthorized\|forbidden" controller.log

# Network issues
grep "timeout\|connection refused\|no such host" controller.log

# Certificate validation errors
grep "x509\|certificate\|verify" controller.log

# Configuration problems
grep "configmap\|secret\|not found" controller.log
```

---

## Getting Help

If you can't resolve the issue:

1. **Collect diagnostic information:**
   ```bash
   # Create a diagnostic bundle
   kubectl get all -n external-issuer-system -o yaml > diag-issuer.yaml
   kubectl get certificates,certificaterequests -A -o yaml > diag-certs.yaml
   kubectl logs -n external-issuer-system deployment/external-issuer-controller > diag-logs.txt
   kubectl describe externalclusterissuers > diag-issuers.txt
   ```

2. **Check the documentation:**
   - [Installation Guide](INSTALLATION.md)
   - [Configuration Guide](CONFIGURATION.md)
   - [How It Works](HOW-IT-WORKS.md)

3. **Open an issue:**
   - Include the diagnostic files
   - Describe the expected vs actual behavior
   - Include Kubernetes and cert-manager versions

4. **Community resources:**
   - cert-manager Slack: [slack.k8s.io](https://slack.k8s.io) → #cert-manager
   - Kubernetes forums: [discuss.kubernetes.io](https://discuss.kubernetes.io)
