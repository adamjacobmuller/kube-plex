# kube-plex Webhook

A MutatingAdmissionWebhook that automatically injects kube-plex transcoder support into Plex Media Server pods.

## How It Works

When a pod with the annotation `kube-plex.io/enabled: "true"` is created, the webhook mutates it to:

1. Add an init container that copies the kube-plex binary
2. Add a transcode PVC volume and mount it to the Plex container
3. Add a postStart lifecycle hook that replaces the Plex Transcoder
4. Inject environment variables for kube-plex configuration

## Prerequisites

- Kubernetes 1.19+
- cert-manager (recommended) or manual TLS certificate generation
- ReadWriteMany storage for the transcode PVC

## Installation

### 1. Deploy the webhook (in kube-plex-system namespace)

#### With cert-manager (recommended)

```bash
# Uncomment certificate.yaml in kustomization.yaml, then:
kubectl apply -k deploy/webhook/

# Add the cert-manager annotation to inject the CA bundle:
kubectl annotate mutatingwebhookconfiguration kube-plex-webhook \
  cert-manager.io/inject-ca-from=kube-plex-system/kube-plex-webhook
```

#### Without cert-manager

```bash
# Create namespace first
kubectl create namespace kube-plex-system

# Generate self-signed certificates
./deploy/webhook/generate-certs.sh

# Apply the manifests
kubectl apply -k deploy/webhook/
```

### 2. Deploy client resources (in your Plex namespace)

```bash
# Edit deploy/client/kustomization.yaml to set your namespace
# Edit deploy/client/rbac.yaml to set your Plex ServiceAccount name

kubectl apply -k deploy/client/
```

This creates:
- `kube-plex-transcode` PVC for transcoder output
- RoleBinding to allow Plex to create transcoder pods

## Usage

### Annotations

Add these annotations to your Plex pod template:

| Annotation | Required | Default | Description |
|------------|----------|---------|-------------|
| `kube-plex.io/enabled` | Yes | - | Set to `"true"` to enable injection |
| `kube-plex.io/transcode-pvc` | No | `kube-plex-transcode` | Name of the transcode PVC |
| `kube-plex.io/transcode-mount` | No | `/transcode` | Mount path for transcode volume |
| `kube-plex.io/data-pvc` | No | auto-detect | Name of the data/media PVC |
| `kube-plex.io/pms-service` | No | - | Service name for PMS internal address |
| `kube-plex.io/pms-container` | No | first container | Name of the PMS container |
| `kube-plex.io/pms-image` | No | container's image | Image for transcoder pods |
| `kube-plex.io/kube-plex-image` | No | `ghcr.io/adamjacobmuller/kube-plex:latest` | kube-plex image |

### Example Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: plex
  annotations:
    kube-plex.io/enabled: "true"
    kube-plex.io/pms-service: "plex"
spec:
  serviceAccountName: plex
  containers:
    - name: plex
      image: plexinc/pms-docker:latest
      volumeMounts:
        - name: config
          mountPath: /config
        - name: data
          mountPath: /data
  volumes:
    - name: config
      persistentVolumeClaim:
        claimName: plex-config
    - name: data
      persistentVolumeClaim:
        claimName: plex-data
```

The webhook will automatically add:
- Transcode PVC volume and mount
- Init container to copy kube-plex binary
- PostStart hook to replace the transcoder
- Environment variables for kube-plex

## Building

```bash
# Build transcoder image
docker build --target transcoder -t ghcr.io/adamjacobmuller/kube-plex:latest .

# Build webhook image
docker build --target webhook -t ghcr.io/adamjacobmuller/kube-plex-webhook:latest .
```
