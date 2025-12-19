# Deploy to AKS with Azure Container Registry
#
# This script builds and deploys the external-issuer to AKS
# using Azure Container Registry for image storage.
#
# Prerequisites:
#   - Azure CLI installed and logged in
#   - kubectl configured with AKS cluster access
#   - cert-manager installed on the cluster
#
# Usage:
#   .\scripts\deploy-to-aks.ps1 -ACRName <acr-name> [-AKSName <aks-name>] [-ResourceGroup <rg>]
#

param(
    [Parameter(Mandatory=$true)]
    [string]$ACRName,
    
    [string]$AKSName,
    [string]$ResourceGroup,
    [string]$Namespace = "external-issuer-system",
    [string]$ImageTag = "latest"
)

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir

function Write-Info { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }

# Validate Azure CLI is installed and logged in
Write-Info "Checking Azure CLI..."
if (-not (Get-Command az -ErrorAction SilentlyContinue)) {
    Write-Err "Azure CLI is not installed"
    exit 1
}

$account = az account show 2>$null | ConvertFrom-Json
if (-not $account) {
    Write-Err "Not logged in to Azure. Run 'az login' first."
    exit 1
}
Write-Info "Logged in as: $($account.user.name)"

# Get ACR login server
Write-Info "Getting ACR information..."
$acr = az acr show --name $ACRName 2>$null | ConvertFrom-Json
if (-not $acr) {
    Write-Err "ACR '$ACRName' not found"
    exit 1
}
$ACRLoginServer = $acr.loginServer
Write-Info "ACR Login Server: $ACRLoginServer"

# Full image name
$FullImageName = "$ACRLoginServer/external-issuer:$ImageTag"

# Build using ACR Tasks (no local Docker needed)
Write-Info "Building image using ACR Tasks..."
Write-Info "Image: $FullImageName"

Push-Location $ProjectDir
az acr build --registry $ACRName --image "external-issuer:$ImageTag" .
Pop-Location

Write-Info "Image built and pushed successfully"

# Get AKS credentials if AKS name provided
if ($AKSName -and $ResourceGroup) {
    Write-Info "Getting AKS credentials..."
    az aks get-credentials --resource-group $ResourceGroup --name $AKSName --overwrite-existing
}

# Verify kubectl access
Write-Info "Verifying cluster access..."
try {
    kubectl cluster-info | Out-Null
} catch {
    Write-Err "Cannot connect to Kubernetes cluster. Ensure kubectl is configured."
    exit 1
}

# Check if cert-manager is installed
$certManagerCRD = kubectl get crd certificates.cert-manager.io 2>$null
if (-not $certManagerCRD) {
    Write-Err "cert-manager is not installed. Please install cert-manager first:"
    Write-Host "  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml"
    exit 1
}

# Apply CRDs
Write-Info "Installing Custom Resource Definitions..."
kubectl apply -f "$ProjectDir\deploy\crds\"

# Apply RBAC
Write-Info "Setting up RBAC..."
kubectl apply -f "$ProjectDir\deploy\rbac\"

# Apply ConfigMap
Write-Info "Creating PKI configuration..."
kubectl apply -f "$ProjectDir\deploy\config\"

# Update deployment with correct image and apply
Write-Info "Deploying controller with image: $FullImageName"
$deploymentContent = Get-Content "$ProjectDir\deploy\deployment.yaml" -Raw
$deploymentContent = $deploymentContent -replace "image: external-issuer:latest", "image: $FullImageName"
$deploymentContent = $deploymentContent -replace "imagePullPolicy: IfNotPresent", "imagePullPolicy: Always"
$deploymentContent | kubectl apply -f -

# Wait for deployment
Write-Info "Waiting for controller to be ready..."
kubectl rollout status deployment/external-issuer-controller -n $Namespace --timeout=180s

# Apply ClusterIssuer
Write-Info "Creating MockCA ClusterIssuer..."
kubectl apply -f "$ProjectDir\deploy\issuer\cluster-issuer.yaml"

# Verify installation
Write-Host ""
Write-Host "=== Deployment Summary ===" -ForegroundColor Cyan
Write-Host ""
Write-Host "ACR: $ACRLoginServer" -ForegroundColor White
Write-Host "Image: $FullImageName" -ForegroundColor White
Write-Host "Namespace: $Namespace" -ForegroundColor White
Write-Host ""

Write-Host "=== Pods ===" -ForegroundColor Cyan
kubectl get pods -n $Namespace

Write-Host ""
Write-Host "=== ClusterIssuers ===" -ForegroundColor Cyan
kubectl get externalclusterissuers

Write-Host ""
Write-Info "Deployment complete!"
Write-Host ""
Write-Host "To test the deployment, create a certificate:"
Write-Host "  kubectl apply -f $ProjectDir\examples\basic-certificate.yaml"
Write-Host ""
Write-Host "Check certificate status:"
Write-Host "  kubectl get certificates"
Write-Host ""
Write-Host "View controller logs:"
Write-Host "  kubectl logs -n $Namespace -l app.kubernetes.io/name=external-issuer -f"
