# CertificateRequest Approval Process

This document explains how CertificateRequest approval works for the external-issuer controller, aligned with cert-manager best practices and the official [sample-external-issuer](https://github.com/cert-manager/sample-external-issuer).

## Overview

cert-manager requires CertificateRequests to be **approved** before an issuer can sign them. This is a security feature that prevents unauthorized certificate issuance.

Key points:
- A signer should **NOT** sign a CertificateRequest without an `Approved` condition
- A signer **WILL** sign a CertificateRequest with an `Approved` condition  
- A signer will **NEVER** sign a CertificateRequest with a `Denied` condition

## Approval Options

There are two main approaches for external issuers:

### Option 1: Use cert-manager's Internal Approver (Recommended)

This is the approach we use, following the sample-external-issuer pattern.

**How it works:**
1. Deploy the approver ClusterRole and ClusterRoleBinding from `deploy/rbac/approver-clusterrole.yaml`
2. This grants cert-manager's internal approver permission to auto-approve CertificateRequests that reference our issuer types

**Required RBAC** (already included in `deploy/rbac/approver-clusterrole.yaml`):

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cert-manager-controller-approve:external-issuer-io
rules:
  - apiGroups: ["cert-manager.io"]
    resources: ["signers"]
    verbs: ["approve"]
    resourceNames:
      - "externalissuers.external-issuer.io/*"
      - "externalclusterissuers.external-issuer.io/*"
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cert-manager-controller-approve:external-issuer-io
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cert-manager-controller-approve:external-issuer-io
subjects:
  - kind: ServiceAccount
    name: cert-manager
    namespace: cert-manager  # Adjust if cert-manager is in a different namespace
```

### Option 2: Configure cert-manager Helm Chart (Alternative)

If you prefer not to deploy additional RBAC, you can configure cert-manager to approve your issuer types via Helm values:

```bash
# When installing/upgrading cert-manager (v1.15.0+)
helm upgrade cert-manager jetstack/cert-manager \
  --set approveSignerNames[0]="issuers.cert-manager.io/*" \
  --set approveSignerNames[1]="clusterissuers.cert-manager.io/*" \
  --set approveSignerNames[2]="externalissuers.external-issuer.io/*" \
  --set approveSignerNames[3]="externalclusterissuers.external-issuer.io/*"
```

### Option 3: Use approver-policy (Enterprise)

For fine-grained approval policies, you can use [approver-policy](https://cert-manager.io/docs/policy/approval/approver-policy/):

```bash
# Install approver-policy
helm install approver-policy jetstack/cert-manager-approver-policy \
  --namespace cert-manager

# Disable cert-manager's internal approver
helm upgrade cert-manager jetstack/cert-manager \
  --set disableAutoApproval=true
```

Then create a CertificateRequestPolicy to control which certificates can be issued.

## RBAC Syntax Explained

The RBAC permission to approve CertificateRequests uses a special syntax:

```
<signer-resource-name>.<signer-group>/<signer-name-or-namespace.name>
```

Examples:
- `externalissuers.external-issuer.io/*` - All ExternalIssuers in all namespaces
- `externalclusterissuers.external-issuer.io/*` - All ExternalClusterIssuers
- `externalissuers.external-issuer.io/default.my-issuer` - Specific issuer in default namespace

## Alignment with Sample External Issuer

Our implementation is aligned with the [cert-manager sample-external-issuer](https://github.com/cert-manager/sample-external-issuer):

| Aspect | Sample External Issuer | Our Implementation |
| ------ | ---------------------- | ------------------- |
| Approval handling | Waits for external approval | ✅ Waits for external approval |
| Approver RBAC | `config/rbac/cert_manager_controller_approver_clusterrole.yaml` | ✅ `deploy/rbac/approver-clusterrole.yaml` |
| Self-approval | No | ✅ No |
| Terminal state check | Yes | ✅ Yes |
| Denied check | Yes | ✅ Yes |

### Key Differences

1. **Library usage**: The sample-external-issuer uses `github.com/cert-manager/issuer-lib` which provides a `CombinedController` with built-in approval handling. Our implementation manually handles this logic.

2. **Architecture**: The sample-external-issuer uses a more modular architecture with:
   - Separate `HealthChecker` and `Signer` interfaces
   - `CombinedController` from issuer-lib
   - Kustomize-based configuration

   Our implementation is more straightforward with direct controller logic.

## Deploying the Approver RBAC

When deploying the external-issuer, apply the approver RBAC:

```bash
# Apply main RBAC
kubectl apply -f deploy/rbac/rbac.yaml

# Apply approver RBAC (grants cert-manager permission to approve)
kubectl apply -f deploy/rbac/approver-clusterrole.yaml
```

## Troubleshooting

### CertificateRequest stuck in pending (not approved)

1. **Check if approver RBAC is deployed:**
   ```bash
   kubectl get clusterrole cert-manager-controller-approve:external-issuer-io
   kubectl get clusterrolebinding cert-manager-controller-approve:external-issuer-io
   ```

2. **Check cert-manager namespace:**
   Ensure the ClusterRoleBinding references the correct namespace where cert-manager is installed.

3. **Check cert-manager logs:**
   ```bash
   kubectl logs -n cert-manager deploy/cert-manager -f | grep -i approve
   ```

### CertificateRequest denied

Check if approver-policy is installed and denying the request:
```bash
kubectl get certificaterequestpolicies
kubectl describe certificaterequest <name>
```

## References

- [cert-manager CertificateRequest Approval](https://cert-manager.io/docs/usage/certificaterequest/#approval)
- [Sample External Issuer](https://github.com/cert-manager/sample-external-issuer)
- [Implementing External Issuers](https://cert-manager.io/docs/contributing/external-issuers/)
- [approver-policy](https://cert-manager.io/docs/policy/approval/approver-policy/)
