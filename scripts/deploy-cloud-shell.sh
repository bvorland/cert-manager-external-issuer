#!/bin/bash
# Deploy External Issuer from Azure Cloud Shell
#
# Usage: ./deploy-cloud-shell.sh <acr-name> <aks-name> <resource-group>
#
# Example:
#   ./deploy-cloud-shell.sh myacr myakscluster my-resource-group
#

set -e

# Check arguments
if [ $# -lt 3 ]; then
    echo "Usage: $0 <acr-name> <aks-name> <resource-group>"
    echo ""
    echo "Example:"
    echo "  $0 myacr myakscluster my-resource-group"
    exit 1
fi

ACR_NAME="$1"
AKS_NAME="$2"
RESOURCE_GROUP="$3"
NAMESPACE="external-issuer-system"
IMAGE_TAG="latest"

# Get script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${CYAN}[$1/7]${NC} $2"; }

echo ""
echo "=============================================="
echo "  External Issuer Deployment - Cloud Shell"
echo "=============================================="
echo ""
echo "ACR:            $ACR_NAME"
echo "AKS:            $AKS_NAME"
echo "Resource Group: $RESOURCE_GROUP"
echo ""

# Step 1: Connect to AKS
log_step 1 "Connecting to AKS cluster..."
az aks get-credentials --resource-group "$RESOURCE_GROUP" --name "$AKS_NAME" --overwrite-existing

echo ""
kubectl get nodes
echo ""

# Step 2: Check cert-manager
log_step 2 "Checking cert-manager..."
if ! kubectl get crd certificates.cert-manager.io &>/dev/null; then
    log_warn "cert-manager not found. Installing..."
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
    log_info "Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=Available deployment --all -n cert-manager --timeout=180s
fi
log_info "cert-manager is ready"

# Step 3: Build image with ACR Tasks
log_step 3 "Building image with ACR Tasks..."
cd "$PROJECT_DIR"
az acr build --registry "$ACR_NAME" --image "external-issuer:$IMAGE_TAG" .
log_info "Image built: $ACR_NAME.azurecr.io/external-issuer:$IMAGE_TAG"

# Step 4: Install CRDs and RBAC
log_step 4 "Installing CRDs and RBAC..."
kubectl apply -f "$PROJECT_DIR/deploy/crds/"
kubectl apply -f "$PROJECT_DIR/deploy/rbac/"

# Step 5: Create configuration
log_step 5 "Creating PKI configuration..."
kubectl apply -f "$PROJECT_DIR/deploy/config/"

# Step 6: Deploy controller
log_step 6 "Deploying controller..."
ACR_IMAGE="$ACR_NAME.azurecr.io/external-issuer:$IMAGE_TAG"
sed "s|image: external-issuer:latest|image: $ACR_IMAGE|g" "$PROJECT_DIR/deploy/deployment.yaml" | kubectl apply -f -

log_info "Waiting for controller to be ready..."
kubectl rollout status deployment/external-issuer-controller -n "$NAMESPACE" --timeout=120s

# Step 7: Create ClusterIssuer
log_step 7 "Creating MockCA ClusterIssuer..."
kubectl apply -f "$PROJECT_DIR/deploy/issuer/cluster-issuer.yaml"

# Summary
echo ""
echo "=============================================="
echo "  Deployment Complete!"
echo "=============================================="
echo ""
echo -e "${CYAN}=== Pods ===${NC}"
kubectl get pods -n "$NAMESPACE"
echo ""
echo -e "${CYAN}=== ClusterIssuers ===${NC}"
kubectl get externalclusterissuers
echo ""
echo "Next steps:"
echo ""
echo "  1. Create a test certificate:"
echo "     kubectl apply -f $PROJECT_DIR/examples/basic-certificate.yaml"
echo ""
echo "  2. Check certificate status:"
echo "     kubectl get certificates"
echo ""
echo "  3. View controller logs:"
echo "     kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=external-issuer -f"
echo ""
