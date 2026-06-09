#!/usr/bin/env bash
# Build all operator services, load them into a Kind cluster, and deploy.
# Creates deployments if they don't exist, updates them if they do.
#
# Services:
#   1. kagenti-controller-manager (operator)
#   2. bundle-service (OPA bundle server)
#
# Usage:
#   ./hack/kind-reload-all.sh [kind-cluster-name] [namespace]
#
# Defaults:
#   cluster:   kagenti
#   namespace: kagenti-system

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

CLUSTER="${1:-kagenti}"
NAMESPACE="${2:-kagenti-system}"
CONTAINER_TOOL="${CONTAINER_TOOL:-docker}"
IMAGE_TAG="$(git -C "${ROOT_DIR}" rev-parse --short HEAD)"

OPERATOR_IMG="localhost/kagenti-operator:${IMAGE_TAG}"
BUNDLE_IMG="localhost/bundle-service:${IMAGE_TAG}"

echo "============================================"
echo " Building and loading to Kind: ${CLUSTER}"
echo " Namespace: ${NAMESPACE}"
echo " Tag: ${IMAGE_TAG}"
echo "============================================"

# --- Ensure namespace ---

echo ""
echo "==> Ensuring namespace '${NAMESPACE}' exists"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# --- Ensure CRDs ---

echo ""
echo "==> Ensuring CRDs are installed"
if [ -f "${ROOT_DIR}/config/crd/bases/agent.kagenti.dev_authorizationpolicies.yaml" ]; then
  kubectl apply -f "${ROOT_DIR}/config/crd/bases/agent.kagenti.dev_authorizationpolicies.yaml"
fi

# --- Build images ---

echo ""
echo "==> Building kagenti-operator image"
${CONTAINER_TOOL} build -t "${OPERATOR_IMG}" -f "${ROOT_DIR}/Dockerfile" "${ROOT_DIR}"

echo ""
echo "==> Building bundle-service image"
${CONTAINER_TOOL} build -t "${BUNDLE_IMG}" -f "${ROOT_DIR}/cmd/bundle-service/Dockerfile" "${ROOT_DIR}"

# --- Load into Kind ---

echo ""
echo "==> Loading images into Kind cluster '${CLUSTER}'"
kind load docker-image "${OPERATOR_IMG}" --name "${CLUSTER}"
kind load docker-image "${BUNDLE_IMG}" --name "${CLUSTER}"

# --- Deploy kagenti-controller-manager ---

echo ""
echo "==> Deploying kagenti-controller-manager"
if kubectl get deployment kagenti-controller-manager -n "${NAMESPACE}" &>/dev/null; then
  kubectl set image deployment/kagenti-controller-manager \
    manager="${OPERATOR_IMG}" \
    -n "${NAMESPACE}"
else
  echo "    Deployment not found — install the operator with 'make deploy' first"
  echo "    (the controller-manager requires webhook certs, RBAC, and CRDs from kustomize)"
fi

# --- Deploy bundle-service ---

echo ""
echo "==> Deploying bundle-service"

# ServiceAccount
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: bundle-service
  namespace: ${NAMESPACE}
EOF

# RBAC
kubectl apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kagenti-bundle-service
rules:
  - apiGroups:
      - agent.kagenti.dev
    resources:
      - authorizationpolicies
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - agent.kagenti.dev
    resources:
      - authorizationpolicies/status
    verbs:
      - get
      - update
      - patch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kagenti-bundle-service
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kagenti-bundle-service
subjects:
  - kind: ServiceAccount
    name: bundle-service
    namespace: ${NAMESPACE}
EOF

# Deployment
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bundle-service
  namespace: ${NAMESPACE}
  labels:
    app: bundle-service
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bundle-service
  template:
    metadata:
      labels:
        app: bundle-service
      annotations:
        sidecar.istio.io/inject: "false"
    spec:
      serviceAccountName: bundle-service
      securityContext:
        runAsNonRoot: true
      containers:
        - name: bundle-service
          image: ${BUNDLE_IMG}
          imagePullPolicy: Never
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          env:
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: LOG_LEVEL
              value: info
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 2
            periodSeconds: 5
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              memory: 256Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
EOF

# Service
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: bundle-service
  namespace: ${NAMESPACE}
spec:
  type: ClusterIP
  selector:
    app: bundle-service
  ports:
    - name: http
      port: 8080
      targetPort: http
      protocol: TCP
EOF

# --- Delete pods to pick up the new images ---

echo ""
echo "==> Deleting pods to pick up new images"
kubectl delete pods -n "${NAMESPACE}" -l app=bundle-service --wait=false
kubectl delete pods -n "${NAMESPACE}" -l control-plane=controller-manager --wait=false 2>/dev/null || true

# --- Wait for rollouts ---

echo ""
echo "==> Waiting for kagenti-controller-manager rollout"
kubectl rollout status deployment/kagenti-controller-manager -n "${NAMESPACE}" --timeout=120s 2>/dev/null || \
  echo "    WARNING: kagenti-controller-manager rollout did not complete"

echo "==> Waiting for bundle-service rollout"
kubectl rollout status deployment/bundle-service -n "${NAMESPACE}" --timeout=60s

# --- Summary ---

echo ""
echo "============================================"
echo " Done!"
echo ""
echo " Images loaded:"
echo "   ${OPERATOR_IMG}"
echo "   ${BUNDLE_IMG}"
echo ""
echo " Namespace: ${NAMESPACE}"
echo "   - kagenti-controller-manager"
echo "   - bundle-service"
echo ""
echo " bundle-service URL: http://bundle-service.${NAMESPACE}.svc.cluster.local:8080"
echo " To port-forward: kubectl port-forward -n ${NAMESPACE} svc/bundle-service 8080:8080"
echo "============================================"
