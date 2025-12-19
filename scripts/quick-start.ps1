# Quick Start Deployment Script for External Issuer (PowerShell)
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
#   .\scripts\quick-start.ps1 [-Build] [-Registry <image>] [-Namespace <ns>]
#

param(
    [switch]$Build,
    [string]$Registry = "external-issuer:latest",
    [string]$Namespace = "external-issuer-system"
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir

function Write-Info { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }

# Check prerequisites
Write-Info "Checking prerequisites..."

if (-not (Get-Command kubectl -ErrorAction SilentlyContinue)) {
    Write-Err "kubectl is not installed"
    exit 1
}

try {
    kubectl cluster-info | Out-Null
} catch {
    Write-Err "Cannot connect to Kubernetes cluster"
    exit 1
}

# Check if cert-manager is installed
$certManagerCRD = kubectl get crd certificates.cert-manager.io 2>$null
if (-not $certManagerCRD) {
    Write-Err "cert-manager is not installed. Please install cert-manager first:"
    Write-Host "  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml"
    exit 1
}

Write-Info "Prerequisites satisfied"

# Build if requested
if ($Build) {
    Write-Info "Building Docker image..."
    Push-Location $ProjectDir
    docker build -t $Registry .
    Pop-Location
    
    # Try to load into kind if available
    if (Get-Command kind -ErrorAction SilentlyContinue) {
        $clusters = kind get clusters 2>$null
        if ($clusters) {
            Write-Info "Loading image into kind cluster..."
            kind load docker-image $Registry
        }
    }
}

# Apply CRDs
Write-Info "Installing Custom Resource Definitions..."
kubectl apply -f "$ProjectDir\deploy\crds\"

# Apply RBAC
Write-Info "Setting up RBAC..."
kubectl apply -f "$ProjectDir\deploy\rbac\"

# Apply ConfigMap
Write-Info "Creating PKI configuration (MockCA mode)..."
kubectl apply -f "$ProjectDir\deploy\config\"

# Apply Deployment
Write-Info "Deploying controller..."
$deploymentContent = Get-Content "$ProjectDir\deploy\deployment.yaml" -Raw
$deploymentContent = $deploymentContent -replace "image: external-issuer:latest", "image: $Registry"
$deploymentContent | kubectl apply -f -

# Wait for deployment
Write-Info "Waiting for controller to be ready..."
kubectl rollout status deployment/external-issuer-controller -n $Namespace --timeout=120s

# Apply ClusterIssuer
Write-Info "Creating MockCA ClusterIssuer..."
kubectl apply -f "$ProjectDir\deploy\issuer\cluster-issuer.yaml"

# Verify
Write-Info "Verifying installation..."
Write-Host ""
Write-Host "=== Pods ===" -ForegroundColor Cyan
kubectl get pods -n $Namespace
Write-Host ""
Write-Host "=== ClusterIssuers ===" -ForegroundColor Cyan
kubectl get externalclusterissuers

Write-Host ""
Write-Info "External Issuer deployed successfully!"
Write-Host ""
Write-Host "Next steps:"
Write-Host "  1. Create a test certificate:"
Write-Host "     kubectl apply -f $ProjectDir\examples\basic-certificate.yaml"
Write-Host ""
Write-Host "  2. Check certificate status:"
Write-Host "     kubectl get certificates"
Write-Host ""
Write-Host "  3. View controller logs:"
Write-Host "     kubectl logs -n $Namespace -l app.kubernetes.io/name=external-issuer -f"
Write-Host ""
