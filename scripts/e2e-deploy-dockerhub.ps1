<#
.SYNOPSIS
    End-to-end deployment script for Docker Hub to AKS

.DESCRIPTION
    This script builds Docker images locally, pushes them to Docker Hub,
    and deploys both the External Issuer controller and MockCA server to AKS.

.PARAMETER DockerHubUser
    Your Docker Hub username (required)

.PARAMETER ImageTag
    Image tag to use (default: latest)

.PARAMETER ResourceGroup
    Azure resource group containing the AKS cluster

.PARAMETER AKSCluster
    Name of the AKS cluster

.PARAMETER SkipBuild
    Skip the local Docker build step

.PARAMETER SkipPush
    Skip pushing to Docker Hub

.EXAMPLE
    .\scripts\e2e-deploy-dockerhub.ps1 -DockerHubUser "myuser" -ResourceGroup "my-rg" -AKSCluster "my-aks"

.EXAMPLE
    .\scripts\e2e-deploy-dockerhub.ps1 -DockerHubUser "myuser" -SkipBuild -ImageTag "v1.0.0"
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$DockerHubUser,
    
    [string]$ImageTag = "latest",
    
    [string]$ResourceGroup,
    
    [string]$AKSCluster,
    
    [switch]$SkipBuild,
    
    [switch]$SkipPush,
    
    [switch]$DeployMockCAOnly,
    
    [switch]$DeployControllerOnly
)

$ErrorActionPreference = "Stop"

# Script directories
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectDir = Split-Path -Parent $ScriptDir

# Image names
$ControllerImage = "external-issuer"
$MockCAImage = "mockca-server"

function Write-Step { param($msg) Write-Host "`n===> $msg" -ForegroundColor Cyan }
function Write-Info { param($msg) Write-Host "[INFO] $msg" -ForegroundColor Green }
function Write-Warn { param($msg) Write-Host "[WARN] $msg" -ForegroundColor Yellow }
function Write-Err { param($msg) Write-Host "[ERROR] $msg" -ForegroundColor Red }

function Test-Command {
    param([string]$Command)
    return [bool](Get-Command $Command -ErrorAction SilentlyContinue)
}

# ============================================================================
# Pre-flight Checks
# ============================================================================

Write-Step "Pre-flight Checks"

# Check Docker
if (-not (Test-Command "docker")) {
    Write-Err "Docker is not installed or not in PATH"
    exit 1
}

$null = docker info 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Err "Docker daemon is not running"
    exit 1
}
Write-Info "Docker is available"

# Check kubectl
if (-not (Test-Command "kubectl")) {
    Write-Err "kubectl is not installed or not in PATH"
    exit 1
}
Write-Info "kubectl is available"

# Check Azure CLI (optional)
if ($ResourceGroup -or $AKSCluster) {
    if (-not (Test-Command "az")) {
        Write-Err "Azure CLI is required when specifying ResourceGroup or AKSCluster"
        exit 1
    }
    Write-Info "Azure CLI is available"
}

# ============================================================================
# Phase 1: Local Docker Build
# ============================================================================

if (-not $SkipBuild) {
    Write-Step "Phase 1: Building Docker Images Locally"
    
    Push-Location $ProjectDir
    
    if (-not $DeployMockCAOnly) {
        Write-Info "Building External Issuer Controller..."
        docker build -t "${ControllerImage}:${ImageTag}" .
        if ($LASTEXITCODE -ne 0) {
            Write-Err "Failed to build controller image"
            Pop-Location
            exit 1
        }
        Write-Info "Controller image built: ${ControllerImage}:${ImageTag}"
    }
    
    if (-not $DeployControllerOnly) {
        Write-Info "Building MockCA Server..."
        docker build -f Dockerfile.mockca -t "${MockCAImage}:${ImageTag}" .
        if ($LASTEXITCODE -ne 0) {
            Write-Err "Failed to build MockCA image"
            Pop-Location
            exit 1
        }
        Write-Info "MockCA image built: ${MockCAImage}:${ImageTag}"
    }
    
    Pop-Location
}
else {
    Write-Step "Phase 1: Skipping Docker Build (--SkipBuild)"
}

# ============================================================================
# Phase 2: Push to Docker Hub
# ============================================================================

if (-not $SkipPush) {
    Write-Step "Phase 2: Pushing Images to Docker Hub"
    
    # Verify Docker Hub login
    Write-Info "Verifying Docker Hub authentication..."
    $dockerConfig = "$env:USERPROFILE\.docker\config.json"
    if (-not (Test-Path $dockerConfig)) {
        Write-Warn "Docker config not found. You may need to run 'docker login'"
    }
    
    if (-not $DeployMockCAOnly) {
        # Tag and push controller
        Write-Info "Tagging and pushing controller image..."
        docker tag "${ControllerImage}:${ImageTag}" "${DockerHubUser}/${ControllerImage}:${ImageTag}"
        docker push "${DockerHubUser}/${ControllerImage}:${ImageTag}"
        if ($LASTEXITCODE -ne 0) {
            Write-Err "Failed to push controller image. Run 'docker login' if authentication failed."
            exit 1
        }
        Write-Info "Pushed: ${DockerHubUser}/${ControllerImage}:${ImageTag}"
    }
    
    if (-not $DeployControllerOnly) {
        # Tag and push MockCA
        Write-Info "Tagging and pushing MockCA image..."
        docker tag "${MockCAImage}:${ImageTag}" "${DockerHubUser}/${MockCAImage}:${ImageTag}"
        docker push "${DockerHubUser}/${MockCAImage}:${ImageTag}"
        if ($LASTEXITCODE -ne 0) {
            Write-Err "Failed to push MockCA image. Run 'docker login' if authentication failed."
            exit 1
        }
        Write-Info "Pushed: ${DockerHubUser}/${MockCAImage}:${ImageTag}"
    }
}
else {
    Write-Step "Phase 2: Skipping Docker Push (--SkipPush)"
}

# ============================================================================
# Phase 3: Connect to AKS
# ============================================================================

Write-Step "Phase 3: Connecting to AKS"

if ($ResourceGroup -and $AKSCluster) {
    Write-Info "Getting AKS credentials..."
    az aks get-credentials --resource-group $ResourceGroup --name $AKSCluster --overwrite-existing
    if ($LASTEXITCODE -ne 0) {
        Write-Err "Failed to get AKS credentials"
        exit 1
    }
}

# Verify cluster connection
Write-Info "Verifying cluster connection..."
kubectl cluster-info | Out-Null
if ($LASTEXITCODE -ne 0) {
    Write-Err "Cannot connect to Kubernetes cluster"
    exit 1
}

$currentContext = kubectl config current-context
Write-Info "Connected to cluster: $currentContext"

# ============================================================================
# Phase 4: Verify cert-manager
# ============================================================================

Write-Step "Phase 4: Verifying cert-manager Installation"

$null = kubectl get crd certificates.cert-manager.io 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Warn "cert-manager CRDs not found. Installing cert-manager..."
    kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.2/cert-manager.yaml
    
    Write-Info "Waiting for cert-manager to be ready..."
    kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=300s
    kubectl wait --for=condition=Available deployment/cert-manager-webhook -n cert-manager --timeout=300s
    kubectl wait --for=condition=Available deployment/cert-manager-cainjector -n cert-manager --timeout=300s
}
else {
    Write-Info "cert-manager is already installed"
}

# ============================================================================
# Phase 5: Deploy MockCA Server
# ============================================================================

if (-not $DeployControllerOnly) {
    Write-Step "Phase 5: Deploying MockCA Server"
    
    # Create temporary manifest with updated image
    $mockCAManifest = Get-Content "$ProjectDir\deploy\mockca-server.yaml" -Raw
    $mockCAManifest = $mockCAManifest -replace "image: mockca-server:latest", "image: ${DockerHubUser}/${MockCAImage}:${ImageTag}"
    $mockCAManifest = $mockCAManifest -replace "imagePullPolicy: IfNotPresent", "imagePullPolicy: Always"
    
    $tempMockCA = [System.IO.Path]::GetTempFileName() + ".yaml"
    $mockCAManifest | Out-File -FilePath $tempMockCA -Encoding utf8
    
    Write-Info "Applying MockCA manifest..."
    kubectl apply -f $tempMockCA
    
    Remove-Item $tempMockCA -Force
    
    Write-Info "Waiting for MockCA to be ready..."
    kubectl wait --for=condition=Available deployment/mockca-server -n mockca-system --timeout=120s
    
    # Verify MockCA
    $mockCAPods = kubectl get pods -n mockca-system -l app.kubernetes.io/name=mockca-server -o jsonpath='{.items[*].status.phase}'
    Write-Info "MockCA pods status: $mockCAPods"
}

# ============================================================================
# Phase 6: Deploy External Issuer Controller
# ============================================================================

if (-not $DeployMockCAOnly) {
    Write-Step "Phase 6: Deploying External Issuer Controller"
    
    # Apply CRDs
    Write-Info "Installing CRDs..."
    kubectl apply -f "$ProjectDir\deploy\crds\"
    
    # Apply RBAC
    Write-Info "Installing RBAC..."
    kubectl apply -f "$ProjectDir\deploy\rbac\"
    
    # Apply PKI config (configured for MockCA)
    Write-Info "Installing PKI configuration..."
    kubectl apply -f "$ProjectDir\deploy\config\pki-config.yaml"
    
    # Create temporary manifest with updated image
    $controllerManifest = Get-Content "$ProjectDir\deploy\deployment.yaml" -Raw
    $controllerManifest = $controllerManifest -replace "image: external-issuer:latest", "image: ${DockerHubUser}/${ControllerImage}:${ImageTag}"
    $controllerManifest = $controllerManifest -replace "imagePullPolicy: IfNotPresent", "imagePullPolicy: Always"
    
    $tempController = [System.IO.Path]::GetTempFileName() + ".yaml"
    $controllerManifest | Out-File -FilePath $tempController -Encoding utf8
    
    Write-Info "Applying controller manifest..."
    kubectl apply -f $tempController
    
    Remove-Item $tempController -Force
    
    Write-Info "Waiting for controller to be ready..."
    kubectl wait --for=condition=Available deployment/external-issuer-controller -n external-issuer-system --timeout=120s
    
    # Verify controller
    $controllerPods = kubectl get pods -n external-issuer-system -l app.kubernetes.io/name=external-issuer -o jsonpath='{.items[*].status.phase}'
    Write-Info "Controller pods status: $controllerPods"
}

# ============================================================================
# Phase 7: Deploy Cluster Issuer
# ============================================================================

if (-not $DeployMockCAOnly) {
    Write-Step "Phase 7: Deploying Cluster Issuer"
    
    kubectl apply -f "$ProjectDir\deploy\issuer\cluster-issuer.yaml"
    
    # Wait a moment for the issuer to be processed
    Start-Sleep -Seconds 5
    
    Write-Info "Cluster issuer status:"
    kubectl get externalclusterissuers
}

# ============================================================================
# Summary
# ============================================================================

Write-Step "Deployment Complete!"

Write-Host "`n" -NoNewline
Write-Host "=" * 60 -ForegroundColor Green
Write-Host "  DEPLOYMENT SUMMARY" -ForegroundColor Green  
Write-Host "=" * 60 -ForegroundColor Green

if (-not $DeployMockCAOnly) {
    Write-Host "`n  Controller Image: " -NoNewline
    Write-Host "${DockerHubUser}/${ControllerImage}:${ImageTag}" -ForegroundColor Yellow
}

if (-not $DeployControllerOnly) {
    Write-Host "  MockCA Image:     " -NoNewline
    Write-Host "${DockerHubUser}/${MockCAImage}:${ImageTag}" -ForegroundColor Yellow
}

Write-Host "`n  Pods Running:" -ForegroundColor Cyan

if (-not $DeployControllerOnly) {
    Write-Host "    MockCA:     " -NoNewline
    kubectl get pods -n mockca-system -l app.kubernetes.io/name=mockca-server --no-headers -o custom-columns=":metadata.name,:status.phase"
}

if (-not $DeployMockCAOnly) {
    Write-Host "    Controller: " -NoNewline
    kubectl get pods -n external-issuer-system -l app.kubernetes.io/name=external-issuer --no-headers -o custom-columns=":metadata.name,:status.phase"
}

Write-Host "`n" -NoNewline
Write-Host "=" * 60 -ForegroundColor Green

Write-Host "`n  Next Steps:" -ForegroundColor Cyan
Write-Host "    1. Create a test certificate:"
Write-Host "       kubectl apply -f examples/basic-certificate.yaml" -ForegroundColor DarkGray
Write-Host "`n    2. Check certificate status:"
Write-Host "       kubectl get certificate example-app-tls" -ForegroundColor DarkGray
Write-Host "`n    3. View logs:"
Write-Host "       kubectl logs -n external-issuer-system -l app.kubernetes.io/name=external-issuer -f" -ForegroundColor DarkGray
Write-Host "       kubectl logs -n mockca-system -l app.kubernetes.io/name=mockca-server -f" -ForegroundColor DarkGray

Write-Host "`n"
