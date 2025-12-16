#!/bin/bash
# Generate self-signed certificates for the webhook
# Use this script if you're not using cert-manager

set -euo pipefail

NAMESPACE="${NAMESPACE:-kube-plex-system}"
SERVICE="${SERVICE:-kube-plex-webhook}"
SECRET="${SECRET:-kube-plex-webhook-tls}"

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Generate CA
openssl genrsa -out "$TMPDIR/ca.key" 2048
openssl req -x509 -new -nodes -key "$TMPDIR/ca.key" -sha256 -days 3650 \
    -out "$TMPDIR/ca.crt" -subj "/CN=kube-plex-webhook-ca"

# Generate server key and CSR
openssl genrsa -out "$TMPDIR/tls.key" 2048
openssl req -new -key "$TMPDIR/tls.key" -out "$TMPDIR/server.csr" \
    -subj "/CN=${SERVICE}.${NAMESPACE}.svc" \
    -config <(cat <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[v3_req]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names
[alt_names]
DNS.1 = ${SERVICE}
DNS.2 = ${SERVICE}.${NAMESPACE}
DNS.3 = ${SERVICE}.${NAMESPACE}.svc
DNS.4 = ${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF
)

# Sign the certificate
openssl x509 -req -in "$TMPDIR/server.csr" -CA "$TMPDIR/ca.crt" -CAkey "$TMPDIR/ca.key" \
    -CAcreateserial -out "$TMPDIR/tls.crt" -days 365 -sha256 \
    -extfile <(cat <<EOF
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = DNS:${SERVICE}, DNS:${SERVICE}.${NAMESPACE}, DNS:${SERVICE}.${NAMESPACE}.svc, DNS:${SERVICE}.${NAMESPACE}.svc.cluster.local
EOF
)

# Create or update the secret
kubectl create secret tls "$SECRET" \
    --namespace="$NAMESPACE" \
    --cert="$TMPDIR/tls.crt" \
    --key="$TMPDIR/tls.key" \
    --dry-run=client -o yaml | kubectl apply -f -

# Output the CA bundle for the webhook configuration
echo ""
echo "CA Bundle (base64 encoded) for MutatingWebhookConfiguration:"
echo ""
base64 < "$TMPDIR/ca.crt" | tr -d '\n'
echo ""
echo ""
echo "To patch the webhook configuration, run:"
echo ""
echo "kubectl patch mutatingwebhookconfiguration kube-plex-webhook --type='json' -p='[{\"op\": \"add\", \"path\": \"/webhooks/0/clientConfig/caBundle\", \"value\": \"'$(base64 < "$TMPDIR/ca.crt" | tr -d '\n')'\"}]'"
