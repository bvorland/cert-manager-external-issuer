# Configuration Guide

This guide explains how to configure the External PKI Issuer to connect to your Certificate Authority (CA) system.

## Configuration Overview

The External Issuer uses a **ConfigMap** to store PKI API connection details. This design allows you to:

- Change PKI endpoints without rebuilding the controller
- Use different configurations for different issuers
- Store sensitive credentials separately in Secrets

## Configuration Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                   external-issuer-system namespace              │
│                                                                  │
│   ┌─────────────────────┐      ┌─────────────────────────────┐  │
│   │   ConfigMap         │      │   Secret                    │  │
│   │   pki-config        │      │   pki-auth                  │  │
│   │                     │      │                             │  │
│   │ • PKI API URL       │      │ • API Token                 │  │
│   │ • HTTP Method       │      │ • Client Certificate        │  │
│   │ • Request Format    │      │ • Password                  │  │
│   │ • Response Parsing  │      │                             │  │
│   └──────────┬──────────┘      └──────────────┬──────────────┘  │
│              │                                │                  │
│              └────────────────┬───────────────┘                  │
│                               │                                  │
│                               ▼                                  │
│              ┌─────────────────────────────────┐                │
│              │   ExternalClusterIssuer         │                │
│              │                                 │                │
│              │ spec:                           │                │
│              │   configMapRef:                 │                │
│              │     name: pki-config            │                │
│              │   authSecretName: pki-auth      │                │
│              └─────────────────────────────────┘                │
└─────────────────────────────────────────────────────────────────┘
```

## PKI Configuration ConfigMap

### Full Configuration Schema

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pki-config
  namespace: external-issuer-system
data:
  pki-config.json: |
    {
      "baseUrl": "https://pki.yourcompany.com/api/v1/certificates",
      "method": "POST",
      "parameters": {
        "newCertParam": "action",
        "newCertValue": "new",
        "renewCertParam": "action", 
        "renewCertValue": "renew",
        "subjectParam": "subject",
        "dnsPrefix": "san_dns",
        "dnsStartIndex": 1,
        "dnsMaxCount": 50,
        "getCertParam": "format",
        "getKeyParam": "",
        "getCSRParam": "csr"
      },
      "response": {
        "format": "pem",
        "certificateField": "certificate",
        "chainField": "chain"
      },
      "auth": {
        "type": "bearer",
        "headerName": "Authorization",
        "secretRef": "pki-auth"
      },
      "tls": {
        "insecureSkipVerify": false,
        "caSecretRef": "pki-ca-cert"
      }
    }
```

### Configuration Fields Explained

#### Base Configuration

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `baseUrl` | string | Yes | Full URL to your PKI API endpoint |
| `method` | string | No | HTTP method: `POST` (default) or `GET` |

#### Parameters Configuration

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `paramFormat` | string | `ampersand` | Parameter format: `ampersand` (key=value&key2=value2) or `semicolon` (key=value;key2=value2) for legacy PKI APIs |
| `subjectDNFormat` | string | `comma` | DN format: `comma` (CN=...,O=...,C=...) or `slash` (/C=.../O=.../CN=...) for legacy PKI APIs |
| `newCertParam` | string | - | Parameter name for new certificate requests |
| `newCertValue` | string | - | Value to send for new certificate requests |
| `renewCertParam` | string | - | Parameter name for renewal requests |
| `renewCertValue` | string | - | Value to send for renewal requests |
| `subjectParam` | string | - | Parameter name for the certificate subject DN |
| `dnsPrefix` | string | - | Prefix for SAN DNS entries (e.g., `san_dns` → `san_dns1`, `san_dns2`) |
| `dnsStartIndex` | int | 1 | Starting index for DNS parameters |
| `dnsMaxCount` | int | 50 | Maximum number of SAN DNS entries |
| `getCertParam` | string | - | Parameter to request certificate in response |
| `getCSRParam` | string | - | Parameter name to send the CSR |

#### Response Configuration

| Field | Type | Default | Description |
| ----- | ---- | ------- | ----------- |
| `format` | string | `pem` | Response format: `pem`, `json`, `base64` |
| `certificateField` | string | - | JSON field containing certificate (if format=json) |
| `chainField` | string | - | JSON field containing CA chain (if format=json) |

#### Authentication Configuration

| Field | Type | Description |
| ----- | ---- | ----------- |
| `type` | string | Auth type: `bearer`, `basic`, `header`, `none` |
| `headerName` | string | Custom header name (for type=header) |
| `secretRef` | string | Name of Secret containing credentials |

#### TLS Configuration

| Field | Type | Description |
| ----- | ---- | ----------- |
| `insecureSkipVerify` | bool | Skip TLS verification (NOT recommended for production) |
| `caSecretRef` | string | Secret containing CA certificate to trust |

## Example Configurations

### Example 1: Simple API with Bearer Token

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pki-config
  namespace: external-issuer-system
data:
  pki-config.json: |
    {
      "baseUrl": "https://ca.example.com/api/sign",
      "method": "POST",
      "parameters": {
        "subjectParam": "subject",
        "dnsPrefix": "dns",
        "dnsStartIndex": 1
      },
      "response": {
        "format": "pem"
      },
      "auth": {
        "type": "bearer",
        "secretRef": "pki-auth"
      }
    }
---
apiVersion: v1
kind: Secret
metadata:
  name: pki-auth
  namespace: external-issuer-system
type: Opaque
stringData:
  token: "your-bearer-token-here"
```

### Example 2: Legacy PKI with Semicolon-Separated Parameters

For legacy PKI APIs that use semicolon-separated parameters and slash-format DNs:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pki-config
  namespace: external-issuer-system
data:
  pki-config.json: |
    {
      "baseUrl": "https://pki.internal.corp/cgi/pki.cgi",
      "method": "POST",
      "parameters": {
        "paramFormat": "semicolon",
        "subjectDNFormat": "slash",
        "newCertParam": "new",
        "newCertValue": "1",
        "renewCertParam": "renew",
        "renewCertValue": "1",
        "subjectParam": "subject",
        "dnsPrefix": "DNS",
        "dnsStartIndex": 2,
        "dnsMaxCount": 20
      },
      "response": {
        "format": "pem"
      },
      "auth": {
        "type": "basic",
        "secretRef": "pki-basic-auth"
      }
    }
---
apiVersion: v1
kind: Secret
metadata:
  name: pki-basic-auth
  namespace: external-issuer-system
type: Opaque
stringData:
  token: "dXNlcm5hbWU6cGFzc3dvcmQ="  # base64(username:password)
```

This configuration generates requests in the following format:
```
POST https://pki.internal.corp/cgi/pki.cgi
Content-Type: application/x-www-form-urlencoded

new=1;subject=/C=US/ST=California/L=San Francisco/O=Example Corp/CN=myapp.example.com;DNS2=myapp2.example.com;DNS3=myapp3.example.com
```

### Example 3: Modern REST API with JSON Response

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pki-config
  namespace: external-issuer-system
data:
  pki-config.json: |
    {
      "baseUrl": "https://api.digicert.com/v1/certificate/issue",
      "method": "POST",
      "parameters": {
        "subjectParam": "common_name",
        "dnsPrefix": "san_",
        "dnsStartIndex": 1
      },
      "response": {
        "format": "json",
        "certificateField": "certificate.pem",
        "chainField": "certificate.chain"
      },
      "auth": {
        "type": "header",
        "headerName": "X-API-Key",
        "secretRef": "digicert-api-key"
      }
    }
```

### Example 4: Development with Mock CA

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: pki-config-mockca
  namespace: external-issuer-system
data:
  pki-config.json: |
    {
      "baseUrl": "http://mock-ca.mock-ca.svc.cluster.local:8080",
      "method": "POST"
    }
```

## Creating the ClusterIssuer

Once your ConfigMap and Secret are configured:

```yaml
apiVersion: external-issuer.io/v1alpha1
kind: ExternalClusterIssuer
metadata:
  name: pki-cluster-issuer
spec:
  # Reference to the ConfigMap
  configMapRef:
    name: pki-config
    namespace: external-issuer-system
    key: pki-config.json
  
  # Reference to auth secret
  authSecretName: pki-auth
  
  # Signer type: "pki" for real PKI, "mockca" for testing
  signerType: pki
```

Apply and verify:

```bash
kubectl apply -f cluster-issuer.yaml
kubectl get externalclusterissuer pki-cluster-issuer
```

## Updating Configuration

### Hot Reload (Recommended)

The controller watches for ConfigMap changes. Simply update the ConfigMap:

```bash
kubectl edit configmap pki-config -n external-issuer-system
```

The controller will pick up changes within ~30 seconds.

### Manual Reload

If needed, restart the controller:

```bash
kubectl rollout restart deployment/external-issuer-controller -n external-issuer-system
```

## Validating Configuration

### Test PKI Connectivity

```bash
# Port-forward to controller for testing
kubectl port-forward -n external-issuer-system deploy/external-issuer-controller 8081:8081

# Check readiness (verifies PKI connection)
curl http://localhost:8081/readyz
```

### Check Controller Logs

```bash
kubectl logs -n external-issuer-system deploy/external-issuer-controller -f
```

Look for:

```
INFO    Successfully connected to PKI API    {"url": "https://pki.example.com"}
INFO    ClusterIssuer is ready    {"name": "pki-cluster-issuer"}
```

## Security Best Practices

1. **Never store credentials in ConfigMap** - Always use Secrets
2. **Use RBAC to restrict Secret access** - Limit who can read PKI credentials
3. **Enable TLS verification** - Set `insecureSkipVerify: false` in production
4. **Use network policies** - Restrict egress to only PKI API endpoints
5. **Rotate credentials regularly** - Update Secrets and restart if needed

## Next Steps

- [Request certificates](USAGE.md)
- [Integrate with Istio](USAGE.md#using-with-istio)
- [Troubleshooting](TROUBLESHOOTING.md)
