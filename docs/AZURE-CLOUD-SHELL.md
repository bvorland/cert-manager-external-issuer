# Deploying with Azure Cloud Shell

This guide shows how to deploy the External Issuer using Azure Cloud Shell - a browser-based shell experience that requires no local installation.

## Table of Contents

- [Why Azure Cloud Shell?](#why-azure-cloud-shell)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Step-by-Step Deployment](#step-by-step-deployment)
- [Building with ACR](#building-with-acr)
- [Verification](#verification)
- [Troubleshooting](#troubleshooting)

---

## Why Azure Cloud Shell?

Azure Cloud Shell provides:

- ✅ Pre-installed tools: `kubectl`, `az`, `helm`, `git`, `docker` (via ACR)
- ✅ No local installation required
- ✅ Persistent storage (5GB in Azure Files)
- ✅ Integrated with Azure Portal
- ✅ Works from any browser or mobile device
- ✅ Automatic Azure authentication

---

## Prerequisites

1. **Azure Subscription** with an AKS cluster
2. **Azure Container Registry (ACR)** attached to your AKS cluster
3. **cert-manager** installed on your AKS cluster

---

## Quick Start

Open Azure Cloud Shell at [shell.azure.com](https://shell.azure.com) and run:

```bash
# Clone the repository
git clone https://github.com/example-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer

# Set your variables
export ACR_NAME="<your-acr-name>"
export AKS_NAME="<your-aks-cluster>"
export RESOURCE_GROUP="<your-resource-group>"

# Connect to AKS
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME

# Build and push image to ACR
az acr build --registry $ACR_NAME --image external-issuer:latest .

# Deploy
./scripts/quick-start.sh --registry "$ACR_NAME.azurecr.io/external-issuer:latest"
```

---

## Step-by-Step Deployment

### 1. Open Azure Cloud Shell

Navigate to [shell.azure.com](https://shell.azure.com) or click the Cloud Shell icon in the Azure Portal.

Choose **Bash** as your shell type.

### 2. Clone the Repository

```bash
# Clone the project
git clone https://github.com/example-org/cert-manager-external-issuer.git
cd cert-manager-external-issuer
```

Or upload the project files using the Cloud Shell upload feature.

### 3. Set Environment Variables

```bash
# Required: Set these to match your environment
export ACR_NAME="<your-acr-name>"           # Just the name, not the full URL
export AKS_NAME="<your-aks-cluster>"
export RESOURCE_GROUP="<your-resource-group>"

# Optional: Customize these
export NAMESPACE="external-issuer-system"
export IMAGE_TAG="latest"
```

### 4. Connect to Your AKS Cluster

```bash
# Get AKS credentials
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME

# Verify connection
kubectl get nodes
```

### 5. Verify cert-manager is Installed

```bash
# Check for cert-manager CRDs
kubectl get crd certificates.cert-manager.io

# If not installed, install it:
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=120s
```

### 6. Build and Push Image to ACR

Azure Cloud Shell doesn't have Docker, but you can use **ACR Tasks** to build in the cloud:

```bash
# Build using ACR Tasks (no local Docker needed!)
az acr build --registry $ACR_NAME --image external-issuer:$IMAGE_TAG .
```

This command:
- Uploads your source code to ACR
- Builds the Docker image in Azure
- Pushes to your ACR automatically

### 7. Deploy the External Issuer

```bash
# Install CRDs
kubectl apply -f deploy/crds/

# Install RBAC
kubectl apply -f deploy/rbac/

# Install ConfigMap (MockCA mode by default)
kubectl apply -f deploy/config/

# Update deployment with your ACR image and deploy
ACR_IMAGE="$ACR_NAME.azurecr.io/external-issuer:$IMAGE_TAG"
sed "s|image: external-issuer:latest|image: $ACR_IMAGE|g" deploy/deployment.yaml | kubectl apply -f -

# Wait for deployment
kubectl rollout status deployment/external-issuer-controller -n $NAMESPACE --timeout=120s

# Install ClusterIssuer
kubectl apply -f deploy/issuer/cluster-issuer.yaml
```

### 8. Verify Installation

```bash
# Check pods
kubectl get pods -n $NAMESPACE

# Check issuers
kubectl get externalclusterissuers

# View logs
kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=external-issuer --tail=50
```

---

## Building with ACR

### One-Command Build and Push

```bash
# Build and push in one command
az acr build \
  --registry $ACR_NAME \
  --image external-issuer:latest \
  --image external-issuer:v1.0.0 \
  .
```

### Check Available Images

```bash
# List images in your ACR
az acr repository show-tags --name $ACR_NAME --repository external-issuer
```

### Using a Specific Image Tag

```bash
# Build with specific tag
az acr build --registry $ACR_NAME --image external-issuer:v1.0.0 .

# Deploy with that tag
ACR_IMAGE="$ACR_NAME.azurecr.io/external-issuer:v1.0.0"
sed "s|image: external-issuer:latest|image: $ACR_IMAGE|g" deploy/deployment.yaml | kubectl apply -f -
```

---

## Verification

### Create a Test Certificate

```bash
# Apply example certificate
kubectl apply -f examples/basic-certificate.yaml

# Watch the certificate status
kubectl get certificate example-app-tls -w
```

### Check Certificate Details

```bash
# Get certificate status
kubectl describe certificate example-app-tls

# View the generated Secret
kubectl get secret example-app-tls-secret -o yaml

# Decode and view the certificate
kubectl get secret example-app-tls-secret -o jsonpath='{.data.tls\.crt}' | \
  base64 -d | openssl x509 -text -noout
```

### Check Controller Logs

```bash
# Follow logs
kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=external-issuer -f

# Search for errors
kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=external-issuer | grep -i error
```

---

## All-in-One Script

Save this as `deploy-cloud-shell.sh` and run it in Cloud Shell:

```bash
#!/bin/bash
# Deploy External Issuer from Azure Cloud Shell
# Usage: ./deploy-cloud-shell.sh <acr-name> <aks-name> <resource-group>

set -e

ACR_NAME="${1:?Usage: $0 <acr-name> <aks-name> <resource-group>}"
AKS_NAME="${2:?Usage: $0 <acr-name> <aks-name> <resource-group>}"
RESOURCE_GROUP="${3:?Usage: $0 <acr-name> <aks-name> <resource-group>}"
NAMESPACE="external-issuer-system"
IMAGE_TAG="latest"

echo "=== External Issuer Deployment ==="
echo "ACR: $ACR_NAME"
echo "AKS: $AKS_NAME"
echo "Resource Group: $RESOURCE_GROUP"
echo ""

# Connect to AKS
echo "[1/7] Connecting to AKS..."
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME --overwrite-existing
kubectl get nodes

# Verify cert-manager
echo "[2/7] Checking cert-manager..."
if ! kubectl get crd certificates.cert-manager.io &>/dev/null; then
    echo "Installing cert-manager..."
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
    kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=180s
fi
echo "cert-manager is ready"

# Build image
echo "[3/7] Building image with ACR Tasks..."
az acr build --registry $ACR_NAME --image external-issuer:$IMAGE_TAG .

# Deploy CRDs and RBAC
echo "[4/7] Installing CRDs and RBAC..."
kubectl apply -f deploy/crds/
kubectl apply -f deploy/rbac/

# Deploy ConfigMap
echo "[5/7] Creating configuration..."
kubectl apply -f deploy/config/

# Deploy controller
echo "[6/7] Deploying controller..."
ACR_IMAGE="$ACR_NAME.azurecr.io/external-issuer:$IMAGE_TAG"
sed "s|image: external-issuer:latest|image: $ACR_IMAGE|g" deploy/deployment.yaml | kubectl apply -f -
kubectl rollout status deployment/external-issuer-controller -n $NAMESPACE --timeout=120s

# Deploy issuer
echo "[7/7] Creating ClusterIssuer..."
kubectl apply -f deploy/issuer/cluster-issuer.yaml

echo ""
echo "=== Deployment Complete ==="
kubectl get pods -n $NAMESPACE
echo ""
kubectl get externalclusterissuers
echo ""
echo "Test with: kubectl apply -f examples/basic-certificate.yaml"
```

Run it:

```bash
chmod +x deploy-cloud-shell.sh
./deploy-cloud-shell.sh youracrname your-aks-cluster your-resource-group
```

---

## Troubleshooting

### "az: command not found"

Make sure you're using Azure Cloud Shell, not a local terminal.

### "Cannot connect to AKS"

```bash
# Verify your subscription
az account show

# List available AKS clusters
az aks list -o table

# Try with admin credentials (if RBAC issues)
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME --admin
```

### ACR Build Fails

```bash
# Check ACR exists
az acr show --name $ACR_NAME

# Check you have push permissions
az acr login --name $ACR_NAME

# Try with verbose output
az acr build --registry $ACR_NAME --image external-issuer:latest . --verbose
```

### AKS Can't Pull from ACR

```bash
# Attach ACR to AKS (requires Owner role on AKS)
az aks update --resource-group $RESOURCE_GROUP --name $AKS_NAME --attach-acr $ACR_NAME

# Or manually create the role assignment
ACR_ID=$(az acr show --name $ACR_NAME --query id -o tsv)
AKS_MI=$(az aks show --resource-group $RESOURCE_GROUP --name $AKS_NAME --query identityProfile.kubeletidentity.clientId -o tsv)
az role assignment create --assignee $AKS_MI --role AcrPull --scope $ACR_ID
```

### Pods Stuck in ImagePullBackOff

```bash
# Check the exact error
kubectl describe pod -n $NAMESPACE -l app.kubernetes.io/name=external-issuer

# Verify image exists in ACR
az acr repository show-tags --name $ACR_NAME --repository external-issuer

# Check AKS-ACR integration
az aks check-acr --resource-group $RESOURCE_GROUP --name $AKS_NAME --acr $ACR_NAME.azurecr.io
```

### Cloud Shell Session Timeout

Cloud Shell sessions timeout after 20 minutes of inactivity. If you get disconnected:

```bash
# Your files are preserved in ~/clouddrive
cd ~/clouddrive/cert-manager-external-issuer

# Reconnect to AKS
az aks get-credentials --resource-group $RESOURCE_GROUP --name $AKS_NAME
```

**Tip:** Store your project in `~/clouddrive/` for persistence across sessions.

---

## Next Steps

- [Configure PKI API](CONFIGURATION.md) - Connect to your real PKI
- [Istio Integration](USAGE.md#using-with-istio) - Use certificates with Istio Gateway
- [Troubleshooting](TROUBLESHOOTING.md) - Common issues and solutions
