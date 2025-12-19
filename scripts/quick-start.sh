#!/bin/bash
# Quick Start Deployment Script for External Issuer
#
# This script deploys the external-issuer to a Kubernetes cluster
# with the MockCA signer for testing purposes.
#
# Prerequisites:
#   - kubectl configured with cluster access
#   - cert-manager v1.12+ installed
#   - Docker (if building from source)
#
# Usage:
#   ./scripts/quick-start.sh [options]
#
# Options:
#   --build       Build the Docker image before deploying
#   --registry    Container registry (default: external-issuer:latest)
#   --namespace   Namespace to deploy to (default: external-issuer-system)
#

set -e

# Default values
NAMESPACE="external-issuer-system"
IMAGE="external-issuer:latest"
BUILD=false
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --build)
            BUILD=true
            shift
            ;;
        --registry)
            IMAGE="$2"
            shift 2
            ;;
        --namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Check prerequisites
log_info "Checking prerequisites..."

if ! command -v kubectl &> /dev/null; then
    log_error "kubectl is not installed"
    exit 1
fi

if ! kubectl cluster-info &> /dev/null; then
    log_error "Cannot connect to Kubernetes cluster"
    exit 1
fi

# Check if cert-manager is installed
if ! kubectl get crd certificates.cert-manager.io &> /dev/null; then
    log_error "cert-manager is not installed. Please install cert-manager first:"
    echo "  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml"
    exit 1
fi

log_info "Prerequisites satisfied"

# Build if requested
if [ "$BUILD" = true ]; then
    log_info "Building Docker image..."
    cd "$PROJECT_DIR"
    docker build -t "$IMAGE" .
    
    # Try to load into kind if available
    if command -v kind &> /dev/null && kind get clusters &> /dev/null; then
        log_info "Loading image into kind cluster..."
        kind load docker-image "$IMAGE"
    fi
fi

# Apply CRDs
log_info "Installing Custom Resource Definitions..."
kubectl apply -f "$PROJECT_DIR/deploy/crds/"

# Apply RBAC
log_info "Setting up RBAC..."
kubectl apply -f "$PROJECT_DIR/deploy/rbac/"

# Apply ConfigMap
log_info "Creating PKI configuration (MockCA mode)..."
kubectl apply -f "$PROJECT_DIR/deploy/config/"

# Update deployment image and apply
log_info "Deploying controller..."
sed "s|image: external-issuer:latest|image: $IMAGE|g" "$PROJECT_DIR/deploy/deployment.yaml" | kubectl apply -f -

# Wait for deployment
log_info "Waiting for controller to be ready..."
kubectl rollout status deployment/external-issuer-controller -n "$NAMESPACE" --timeout=120s

# Apply ClusterIssuer
log_info "Creating MockCA ClusterIssuer..."
kubectl apply -f "$PROJECT_DIR/deploy/issuer/cluster-issuer.yaml"

# Verify
log_info "Verifying installation..."
echo ""
echo "=== Pods ==="
kubectl get pods -n "$NAMESPACE"
echo ""
echo "=== ClusterIssuers ==="
kubectl get externalclusterissuers

echo ""
log_info "External Issuer deployed successfully!"
echo ""
echo "Next steps:"
echo "  1. Create a test certificate:"
echo "     kubectl apply -f $PROJECT_DIR/examples/basic-certificate.yaml"
echo ""
echo "  2. Check certificate status:"
echo "     kubectl get certificates"
echo ""
echo "  3. View controller logs:"
echo "     kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=external-issuer -f"
echo ""
