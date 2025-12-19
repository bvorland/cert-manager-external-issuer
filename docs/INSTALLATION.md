# Installation Guide

This guide covers deploying the External PKI Issuer to a Kubernetes cluster.

## Prerequisites

Before installing, ensure you have:

- [ ] Kubernetes cluster (1.25+)
- [ ] `kubectl` configured with cluster-admin access
- [ ] cert-manager v1.12+ installed
- [ ] Container registry access (ACR, Docker Hub, etc.)

### Verify cert-manager

```bash
kubectl get pods -n cert-manager
# All pods should be Running

kubectl get crd | grep cert-manager
# Should show certificaterequests.cert-manager.io, certificates.cert-manager.io, etc.
```

## Quick Start (MockCA for Testing)

For quick testing without configuring a real PKI, use the built-in **MockCA** signer:

```bash
# Clone the repository
git clone https://github.com/your-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer

# Deploy everything (CRDs, RBAC, controller)
kubectl apply -f deploy/crds/
kubectl apply -f deploy/rbac/
kubectl apply -f deploy/config/
kubectl apply -f deploy/deployment.yaml
kubectl apply -f deploy/issuer/cluster-issuer.yaml

# Verify issuer is ready
kubectl get externalclusterissuer mockca-cluster-issuer
# NAME                    READY   REASON    AGE
# mockca-cluster-issuer   True    Success   10s
```

> **Note**: MockCA generates self-signed certificates locally - perfect for testing.
> For production, configure the PKI signer with your enterprise CA.

## Installation Methods

Choose the installation method that best fits your environment:

| Method | Best For |
| ------ | -------- |
| [Azure Cloud Shell](#method-1-azure-cloud-shell) | AKS users, no local tools needed |
| [Local with Manifests](#method-2-deploy-from-manifests) | Any Kubernetes cluster |
| [Build and Deploy](#method-3-build-and-deploy) | Custom builds, development |
| [PowerShell Script](#method-4-powershell-script) | Windows users with AKS |

---

### Method 1: Azure Cloud Shell

**No local tools required!** Deploy directly from your browser.

1. Open [shell.azure.com](https://shell.azure.com)
2. Clone and deploy:

```bash
# Clone the repository
git clone https://github.com/your-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer

# Set your variables
export ACR_NAME="youracrname"
export AKS_NAME="your-aks-cluster"
export RESOURCE_GROUP="your-resource-group"

# Connect to AKS
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME

# Build with ACR Tasks and deploy
az acr build --registry $ACR_NAME --image external-issuer:latest .
./scripts/quick-start.sh --registry "$ACR_NAME.azurecr.io/external-issuer:latest"
```

📖 **Full Guide:** [AZURE-CLOUD-SHELL.md](AZURE-CLOUD-SHELL.md)

---

### Method 2: Deploy from Manifests

```bash
# Clone the repository
git clone https://github.com/your-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer

# Deploy everything
kubectl apply -f deploy/
```

---

### Method 3: Build and Deploy

If you need to customize the controller:

```bash
# Build the image
docker build -t your-registry.com/external-issuer:v1 .

# Push to your registry
docker push your-registry.com/external-issuer:v1

# Update the deployment with your image
sed -i 's|ghcr.io/your-org/external-issuer:latest|your-registry.com/external-issuer:v1|g' deploy/deployment.yaml

# Deploy
kubectl apply -f deploy/
```

---

### Method 4: PowerShell Script

For Windows users deploying to AKS:

```powershell
# Clone and deploy
git clone https://github.com/your-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer

# Deploy to AKS with ACR
.\scripts\deploy-to-aks.ps1 -ACRName "youracr" -AKSName "your-aks" -ResourceGroup "your-rg"
```

---

### Method 5: Helm Chart (Coming Soon)

```bash
helm repo add external-issuer https://your-org.github.io/external-issuer
helm install external-issuer external-issuer/external-issuer \
  --namespace external-issuer-system \
  --create-namespace
```

## Step-by-Step Installation

### Step 1: Create Namespace

```bash
kubectl create namespace external-issuer-system
```

### Step 2: Apply CRDs

```bash
kubectl apply -f deploy/crds/
```

Verify CRDs are installed:

```bash
kubectl get crd | grep external-issuer
# Should show:
# externalclusterissuers.external-issuer.io
# externalissuers.external-issuer.io
```

### Step 3: Apply RBAC

```bash
# Apply controller RBAC
kubectl apply -f deploy/rbac/rbac.yaml

# Apply approver RBAC (grants cert-manager permission to approve CertificateRequests)
kubectl apply -f deploy/rbac/approver-clusterrole.yaml
```

> **Note**: The `approver-clusterrole.yaml` grants cert-manager's internal approver permission to auto-approve CertificateRequests that reference our issuer types. If cert-manager is installed in a different namespace (e.g., `plat-system`), update the namespace in the ClusterRoleBinding.

### Step 4: Configure PKI Connection

Create the PKI configuration ConfigMap:

```bash
kubectl apply -f deploy/config/pki-config.yaml
```

If using authentication, create the secret:

```bash
kubectl create secret generic pki-auth \
  --namespace external-issuer-system \
  --from-literal=token='your-api-token'
```

### Step 5: Deploy Controller

```bash
kubectl apply -f deploy/deployment.yaml
```

### Step 6: Verify Installation

```bash
# Check pod is running
kubectl get pods -n external-issuer-system
# NAME                                 READY   STATUS    RESTARTS   AGE
# external-issuer-controller-xxxxx     1/1     Running   0          1m

# Check logs
kubectl logs -n external-issuer-system deploy/external-issuer-controller
```

### Step 7: Create ClusterIssuer

For testing with the built-in MockCA (self-signing, no external PKI needed):

```yaml
apiVersion: external-issuer.io/v1alpha1
kind: ExternalClusterIssuer
metadata:
  name: mockca-cluster-issuer
spec:
  signerType: mockca
```

For production with your enterprise PKI:

```yaml
apiVersion: external-issuer.io/v1alpha1
kind: ExternalClusterIssuer
metadata:
  name: pki-cluster-issuer
spec:
  signerType: pki
  configMapRef:
    name: external-issuer-config
    namespace: external-issuer-system
  authSecretName: pki-api-credentials
```

Apply the issuer:

```bash
kubectl apply -f deploy/issuer/cluster-issuer.yaml
```

Verify the issuer is ready:

```bash
kubectl get externalclusterissuer
# NAME                    READY   REASON    AGE
# mockca-cluster-issuer   True    Success   1m
```

## Test Certificate Issuance

Create a test certificate to verify everything works:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-certificate
  namespace: default
spec:
  secretName: test-cert-tls
  duration: 2160h
  renewBefore: 360h
  commonName: test.example.com
  dnsNames:
    - test.example.com
    - www.test.example.com
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
```

```bash
kubectl apply -f - <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-certificate
  namespace: default
spec:
  secretName: test-cert-tls
  commonName: test.example.com
  dnsNames:
    - test.example.com
  issuerRef:
    name: mockca-cluster-issuer
    kind: ExternalClusterIssuer
    group: external-issuer.io
EOF

# Verify certificate is issued
kubectl get certificate test-certificate
# NAME               READY   SECRET          AGE
# test-certificate   True    test-cert-tls   30s

# Check the secret was created
kubectl get secret test-cert-tls
```

## Troubleshooting Installation

### CertificateRequest stuck "Not Approved"

The external-issuer controller automatically approves CertificateRequests for its issuer types.
If requests are stuck, check:

```bash
# Verify RBAC is correct
kubectl get clusterrole external-issuer-controller-role -o yaml | grep -A5 signers

# Check controller logs
kubectl logs -n external-issuer-system deploy/external-issuer-controller
```

### ClusterIssuer shows "HealthCheckFailed"

For **MockCA**: This should work automatically. If failing, restart the controller:
```bash
kubectl rollout restart deployment/external-issuer-controller -n external-issuer-system
```

For **PKI mode**: Ensure your ConfigMap has the correct format with a `pki-config.json` key containing valid JSON configuration.

## Installation on AKS (Azure Kubernetes Service)

### Additional AKS Considerations

1. **Private AKS Clusters**: Ensure network connectivity from nodes to PKI API
2. **ACR Integration**: Use managed identity for pulling images

```bash
# Attach ACR to AKS
az aks update -n myAKSCluster -g myResourceGroup --attach-acr myACR

# Push image to ACR
az acr login --name myACR
docker tag external-issuer:v1 myACR.azurecr.io/external-issuer:v1
docker push myACR.azurecr.io/external-issuer:v1
```

3. **Pod Identity**: If your PKI requires Azure AD authentication

```yaml
# Add to deployment
spec:
  template:
    metadata:
      labels:
        azure.workload.identity/use: "true"
```

## Uninstallation

```bash
# Remove all resources
kubectl delete -f deploy/

# Remove CRDs (will delete all issuers!)
kubectl delete -f deploy/crds/

# Remove namespace
kubectl delete namespace external-issuer-system
```

## Next Steps

- [Configure PKI connection](CONFIGURATION.md)
- [Request your first certificate](USAGE.md)
- [Integrate with Istio](USAGE.md#using-with-istio)
