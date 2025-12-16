# kube-plex Webhook

A MutatingAdmissionWebhook that automatically injects kube-plex transcoder support into Plex Media Server pods.

## How It Works

When a pod with the annotation `kube-plex.io/enabled: "true"` is created, the webhook mutates it to:

1. Add an init container that copies the kube-plex binary
2. Add a postStart lifecycle hook that replaces the Plex Transcoder
3. Inject environment variables for kube-plex configuration

## Prerequisites

- Kubernetes 1.19+
- cert-manager (recommended) or manual TLS certificate generation

## Installation

### With cert-manager (recommended)

```bash
# Uncomment certificate.yaml in kustomization.yaml, then:
kubectl apply -k deploy/webhook/

# Add the cert-manager annotation to inject the CA bundle:
kubectl annotate mutatingwebhookconfiguration kube-plex-webhook \
  cert-manager.io/inject-ca-from=kube-plex-system/kube-plex-webhook
```

### Without cert-manager

```bash
# Create namespace first
kubectl create namespace kube-plex-system

# Generate self-signed certificates
./deploy/webhook/generate-certs.sh

# Apply the manifests
kubectl apply -k deploy/webhook/
```

## Usage

### Annotations

Add these annotations to your Plex pod:

| Annotation | Required | Description |
|------------|----------|-------------|
| `kube-plex.io/enabled` | Yes | Set to `"true"` to enable injection |
| `kube-plex.io/transcode-pvc` | Yes* | Name of the transcode PVC |
| `kube-plex.io/data-pvc` | No | Name of the data PVC (auto-detected if volume named "data" exists) |
| `kube-plex.io/config-pvc` | No | Name of the config PVC (auto-detected if volume named "config" exists) |
| `kube-plex.io/pms-service` | No | Service name for PMS internal address |
| `kube-plex.io/pms-container` | No | Name of the PMS container (defaults to first container) |
| `kube-plex.io/pms-image` | No | Image for transcoder pods (defaults to container's image) |
| `kube-plex.io/kube-plex-image` | No | kube-plex image (defaults to `ghcr.io/munnerz/kube-plex:latest`) |

*The transcode PVC can be auto-detected if a volume named "transcode" exists.

### Example Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: plex
  annotations:
    kube-plex.io/enabled: "true"
    kube-plex.io/transcode-pvc: "plex-transcode"
    kube-plex.io/pms-service: "plex"
spec:
  containers:
    - name: plex
      image: plexinc/pms-docker:latest
      volumeMounts:
        - name: config
          mountPath: /config
        - name: data
          mountPath: /data
        - name: transcode
          mountPath: /transcode
  volumes:
    - name: config
      persistentVolumeClaim:
        claimName: plex-config
    - name: data
      persistentVolumeClaim:
        claimName: plex-data
    - name: transcode
      persistentVolumeClaim:
        claimName: plex-transcode
```

### RBAC for Transcoder Pods

The Plex pod's ServiceAccount needs permission to create transcoder pods. Bind the `kube-plex-transcoder` ClusterRole:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: plex-kube-plex
  namespace: plex
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: kube-plex-transcoder
subjects:
  - kind: ServiceAccount
    name: plex
    namespace: plex
```

## Building

```bash
# Build transcoder image
docker build --target transcoder -t ghcr.io/adamjacobmuller/kube-plex:latest .

# Build webhook image
docker build --target webhook -t ghcr.io/adamjacobmuller/kube-plex-webhook:latest .
```
