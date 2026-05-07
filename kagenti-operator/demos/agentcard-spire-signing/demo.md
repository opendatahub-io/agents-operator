# SPIRE Signing Demo

This demo shows automated AgentCard signing via a SPIRE init-container and operator-side x5c signature verification with trust-domain identity binding.

## Overview

```
                  SPIRE Server
                       |
                  issues X.509-SVID
                       |
                       v
Agent Pod                                    Operator Pod
+---------------------------+                +---------------------------+
|                           |                |                           |
|  init: sign-agentcard     |                |  agentcard-operator       |
|    fetches SVID from SPIRE|                |    fetches /.well-known/  |
|    signs card with JWS    |                |    verifies x5c chain     |
|    writes to shared vol   |                |    validates trust domain |
|                           |                |    sets Verified + Bound  |
|  main: serves signed card |  <-- fetch --- |                           |
|    at /.well-known/       |                |                           |
+---------------------------+                +---------------------------+
```

The operator verifies the JWS signature using the x5c certificate chain embedded in the protected header, then validates that the leaf certificate's SPIFFE ID belongs to the configured trust domain.

## Prerequisites

- Kubernetes cluster with SPIRE installed (e.g. `kagenti/scripts/kind/setup-kagenti.sh --with-spire`)
- kagenti-operator deployed with the following signature verification flags:

```bash
--require-a2a-signature=true
--enforce-network-policies=true
--spire-trust-domain=<your-trust-domain> # 'localtest.me' in Kind by default
--spire-trust-bundle-configmap=spire-bundle
--spire-trust-bundle-configmap-namespace=<spire-bundle-namespace> # 'spire-system' in Kind, 'zero-trust-workload-identity-manager' in OpenShift by default
```

If SPIRE was installed alongside Kagenti with the script above or similar, you can run the following helm command to apply the required flags:

```bash
KAGENTI_REPO=<path-to-your-kagenti-repo>
helm upgrade kagenti "$KAGENTI_REPO/charts/kagenti/" \
  -n kagenti-system \
  --reuse-values \
  -f "$KAGENTI_REPO/charts/kagenti/.secrets.yaml" \
  --set kagenti-operator-chart.signatureVerification.enabled=true \
  --set kagenti-operator-chart.signatureVerification.enforceNetworkPolicies=true \
  --set kagenti-operator-chart.signatureVerification.spireTrustDomain=<your-trust-domain> \
  --set kagenti-operator-chart.signatureVerification.spireTrustBundle.configMapName=spire-bundle \
  --set kagenti-operator-chart.signatureVerification.spireTrustBundle.configMapNamespace=<spire-bundle-namespace>
```

If you are using OpenShift, include the following flag to use the correct bundle data field:

```bash
  --set kagenti-operator-chart.signatureVerification.spireTrustBundle.configMapKey=bundle.crt
```

You can check the name of the spire domain with the following command:

```bash
kubectl get configmap spire-server -n zero-trust-workload-identity-manager -o jsonpath='{.data.server\.conf}{"\n"}' | grep trust_domain
```

## Setup

### 1. Deploy the Demo

```bash
cd <path-to-your-kagenti-operator-repo>/kagenti-operator
kubectl apply -f demos/agentcard-spire-signing/k8s/namespace.yaml
kubectl apply -f demos/agentcard-spire-signing/k8s/clusterspiffeid.yaml
kubectl apply -f demos/agentcard-spire-signing/k8s/agent-deployment.yaml
```

### 2. Wait for Pods

```bash
kubectl wait --for=condition=available --timeout=120s deployment/weather-agent -n agents
```

### 3. Test the Flow

Run the demo script to see signing and verification in action:

```bash
./demos/agentcard-spire-signing/run-demo-commands.sh
```

Expected output:

```
=== 1. Init-Container Signing Logs ===
{"level":"info","msg":"starting agentcard signer",...}
{"level":"info","msg":"fetched SVID","spiffe_id":"spiffe://<domain>/ns/agents/sa/weather-agent-sa",...}
{"level":"info","msg":"signed card written successfully",...}

=== 2. Signed Card Verification ===
  Name:       Weather Agent
  Signed:     True
  Signatures: 1

=== 3. JWS Protected Header ===
  Algorithm:  ES256
  Type:       JOSE
  Key ID:     <16-char hex>
  x5c certs:  1

=== 4. Operator Verification Status ===
  SignatureVerified: True  (SignatureValid)
  Bound:             True  (Bound)
  Synced:            True  (SyncSucceeded)

=== 5. Identity Binding ===
  SPIFFE ID:      spiffe://<domain>/ns/agents/sa/weather-agent-sa
  Identity Match: True
  Bound:          True

=== 6. Signature Label ===
  agent.kagenti.dev/signature-verified: true

=== 7. AgentCard Summary ===
NAME                PROTOCOL   KIND         TARGET          AGENT            VERIFIED   BOUND   SYNCED   ...
weather-agent-deployment-card  a2a        Deployment   weather-agent   Weather Agent    true       true    True     ...
```

## How It Works

1. The `sign-agentcard` init-container fetches an X.509-SVID from SPIRE via the Workload API
2. It signs the unsigned AgentCard JSON with JWS (ES256), embedding the certificate chain in the `x5c` header
3. The signed card is written to a shared `emptyDir` volume
4. The main container serves the signed card at `/.well-known/agent-card.json`
5. The operator fetches the card, verifies the JWS signature against the SPIRE trust bundle
6. The operator extracts the SPIFFE ID from the leaf certificate's SAN URI
7. If the SPIFFE ID belongs to the configured trust domain, the card is marked as Bound
8. The `agent.kagenti.dev/signature-verified` label is set on the workload

## Cleanup

Use the teardown script to delete all demo resources:

```bash
./demos/agentcard-spire-signing/teardown-demo.sh
```

Or manually:

```bash
kubectl delete -f demos/agentcard-spire-signing/k8s/agent-deployment.yaml
kubectl delete -f demos/agentcard-spire-signing/k8s/clusterspiffeid.yaml
kubectl delete -f demos/agentcard-spire-signing/k8s/namespace.yaml
```

## Troubleshooting

### Pull rate limit error for `docker.io/python:3.11-slim`

If you run into image pull rate limit on OpenShift, you can patch the deployment to use a Red Hat UBI Python image:

```bash
oc patch deployment weather-agent -n agents --type=json -p='[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":"registry.redhat.io/ubi9/python-311:latest"}]'
```

### Error pulling `agentcard-signer` image for `ghcr.io`

You can build your own image and upload it to Kind/OpenShift internal registry with the following commands:

```bash
cd kagenti-operator/

# Kind
make build-signer # Build the signer image
make load-signer-image KIND_CLUSTER_NAME=kagenti # Load the signer image into the default "kagenti" cluster

# OpenShift
oc new-build -n agents --name agentcard-signer --binary --strategy docker --to=agentcard-signer # Create a binary BuildConfig that outputs the signer image
oc patch bc/agentcard-signer -n agents --type=json -p='[{"op":"add","path":"/spec/strategy/dockerStrategy/dockerfilePath","value":"cmd/agentcard-signer/Dockerfile"}]' # Point the BuildConfig to the signer Dockerfile path in this repo
oc start-build agentcard-signer -n agents --from-dir=. # Upload the current directory as build context and start the image build
oc patch deployment weather-agent -n agents --type=json -p='[{"op":"replace","path":"/spec/template/spec/initContainers/0/image","value":"image-registry.openshift-image-registry.svc:5000/agents/agentcard-signer:latest"}]' # Use the newly built signer image from the OpenShift internal registry
```
