package webhook

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// Annotations
	AnnotationEnabled       = "kube-plex.io/enabled"
	AnnotationPMSService    = "kube-plex.io/pms-service"
	AnnotationDataPVC       = "kube-plex.io/data-pvc"
	AnnotationConfigPVC     = "kube-plex.io/config-pvc"
	AnnotationTranscodePVC  = "kube-plex.io/transcode-pvc"
	AnnotationPMSContainer  = "kube-plex.io/pms-container"
	AnnotationPMSImage      = "kube-plex.io/pms-image"
	AnnotationKubePlexImage = "kube-plex.io/kube-plex-image"

	// Defaults
	DefaultKubePlexImage = "ghcr.io/adamjacobmuller/kube-plex:latest"

	// Volume/mount names
	kubePlexBinaryVolume = "kube-plex-binary"
	kubePlexBinaryMount  = "/shared"
)

// PatchOperation represents a JSON patch operation
type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

// Config holds the configuration extracted from annotations
type Config struct {
	PMSService    string
	DataPVC       string
	ConfigPVC     string
	TranscodePVC  string
	PMSContainer  string
	PMSImage      string
	KubePlexImage string
	Namespace     string
}

// ShouldMutate checks if the pod should be mutated based on annotations
func ShouldMutate(pod *corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	return strings.ToLower(pod.Annotations[AnnotationEnabled]) == "true"
}

// ExtractConfig extracts configuration from pod annotations and spec
func ExtractConfig(pod *corev1.Pod, namespace string) (*Config, error) {
	cfg := &Config{
		Namespace: namespace,
	}

	annotations := pod.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Get PMS service name (required or derive from pod name)
	cfg.PMSService = annotations[AnnotationPMSService]

	// Get PVC names - try annotations first, then auto-detect from volumes
	cfg.DataPVC = annotations[AnnotationDataPVC]
	cfg.ConfigPVC = annotations[AnnotationConfigPVC]
	cfg.TranscodePVC = annotations[AnnotationTranscodePVC]

	// Auto-detect PVCs from volume mounts if not specified
	if cfg.DataPVC == "" || cfg.ConfigPVC == "" || cfg.TranscodePVC == "" {
		detectPVCs(pod, cfg)
	}

	// Validate required PVCs
	if cfg.TranscodePVC == "" {
		return nil, fmt.Errorf("transcode PVC is required: set %s annotation", AnnotationTranscodePVC)
	}

	// Get container name (default to first container)
	cfg.PMSContainer = annotations[AnnotationPMSContainer]
	if cfg.PMSContainer == "" && len(pod.Spec.Containers) > 0 {
		cfg.PMSContainer = pod.Spec.Containers[0].Name
	}

	// Get PMS image (default to the container's image)
	cfg.PMSImage = annotations[AnnotationPMSImage]
	if cfg.PMSImage == "" {
		for _, c := range pod.Spec.Containers {
			if c.Name == cfg.PMSContainer {
				cfg.PMSImage = c.Image
				break
			}
		}
	}

	// Get kube-plex image
	cfg.KubePlexImage = annotations[AnnotationKubePlexImage]
	if cfg.KubePlexImage == "" {
		cfg.KubePlexImage = DefaultKubePlexImage
	}

	return cfg, nil
}

// detectPVCs attempts to auto-detect PVC names from volume mounts
func detectPVCs(pod *corev1.Pod, cfg *Config) {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim == nil {
			continue
		}
		pvcName := vol.PersistentVolumeClaim.ClaimName

		// Try to match by volume name or mount path
		switch strings.ToLower(vol.Name) {
		case "data":
			if cfg.DataPVC == "" {
				cfg.DataPVC = pvcName
			}
		case "config":
			if cfg.ConfigPVC == "" {
				cfg.ConfigPVC = pvcName
			}
		case "transcode":
			if cfg.TranscodePVC == "" {
				cfg.TranscodePVC = pvcName
			}
		}
	}
}

// CreatePatch creates the JSON patch to mutate the pod
func CreatePatch(pod *corev1.Pod, cfg *Config) ([]byte, error) {
	var patches []PatchOperation

	// Find the PMS container index
	pmsContainerIdx := -1
	for i, c := range pod.Spec.Containers {
		if c.Name == cfg.PMSContainer {
			pmsContainerIdx = i
			break
		}
	}
	if pmsContainerIdx == -1 {
		return nil, fmt.Errorf("container %q not found", cfg.PMSContainer)
	}

	// 1. Add emptyDir volume for kube-plex binary
	patches = append(patches, addVolume(pod, corev1.Volume{
		Name: kubePlexBinaryVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}))

	// 2. Add init container to copy kube-plex binary
	initContainer := corev1.Container{
		Name:    "kube-plex-init",
		Image:   cfg.KubePlexImage,
		Command: []string{"cp", "/kube-plex", kubePlexBinaryMount + "/kube-plex"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      kubePlexBinaryVolume,
				MountPath: kubePlexBinaryMount,
			},
		},
	}
	patches = append(patches, addInitContainer(pod, initContainer))

	// 3. Add volume mount for kube-plex binary to PMS container
	patches = append(patches, addVolumeMount(pod, pmsContainerIdx, corev1.VolumeMount{
		Name:      kubePlexBinaryVolume,
		MountPath: kubePlexBinaryMount,
	}))

	// 4. Add postStart lifecycle hook to replace transcoder
	patches = append(patches, addLifecycleHook(pod, pmsContainerIdx))

	// 5. Add environment variables for kube-plex
	envVars := buildEnvVars(cfg)
	for _, env := range envVars {
		patches = append(patches, addEnvVar(pod, pmsContainerIdx, env))
	}

	return json.Marshal(patches)
}

func addVolume(pod *corev1.Pod, volume corev1.Volume) PatchOperation {
	if len(pod.Spec.Volumes) == 0 {
		return PatchOperation{
			Op:    "add",
			Path:  "/spec/volumes",
			Value: []corev1.Volume{volume},
		}
	}
	return PatchOperation{
		Op:    "add",
		Path:  "/spec/volumes/-",
		Value: volume,
	}
}

func addInitContainer(pod *corev1.Pod, container corev1.Container) PatchOperation {
	if len(pod.Spec.InitContainers) == 0 {
		return PatchOperation{
			Op:    "add",
			Path:  "/spec/initContainers",
			Value: []corev1.Container{container},
		}
	}
	return PatchOperation{
		Op:    "add",
		Path:  "/spec/initContainers/-",
		Value: container,
	}
}

func addVolumeMount(pod *corev1.Pod, containerIdx int, mount corev1.VolumeMount) PatchOperation {
	path := fmt.Sprintf("/spec/containers/%d/volumeMounts", containerIdx)
	if len(pod.Spec.Containers[containerIdx].VolumeMounts) == 0 {
		return PatchOperation{
			Op:    "add",
			Path:  path,
			Value: []corev1.VolumeMount{mount},
		}
	}
	return PatchOperation{
		Op:    "add",
		Path:  path + "/-",
		Value: mount,
	}
}

func addLifecycleHook(pod *corev1.Pod, containerIdx int) PatchOperation {
	path := fmt.Sprintf("/spec/containers/%d/lifecycle", containerIdx)

	lifecycle := &corev1.Lifecycle{
		PostStart: &corev1.LifecycleHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"/bin/sh", "-c",
					// Wait for transcoder to exist, then replace it
					`until [ -f "/usr/lib/plexmediaserver/Plex Transcoder" ]; do sleep 1; done; ` +
						`cp "` + kubePlexBinaryMount + `/kube-plex" "/usr/lib/plexmediaserver/Plex Transcoder"`,
				},
			},
		},
	}

	// If lifecycle already exists, we need to merge
	if pod.Spec.Containers[containerIdx].Lifecycle != nil {
		existing := pod.Spec.Containers[containerIdx].Lifecycle
		if existing.PostStart != nil {
			// Wrap existing postStart with our command
			lifecycle.PostStart.Exec.Command = []string{
				"/bin/sh", "-c",
				`until [ -f "/usr/lib/plexmediaserver/Plex Transcoder" ]; do sleep 1; done; ` +
					`cp "` + kubePlexBinaryMount + `/kube-plex" "/usr/lib/plexmediaserver/Plex Transcoder"`,
			}
		}
		lifecycle.PreStop = existing.PreStop
		return PatchOperation{
			Op:    "replace",
			Path:  path,
			Value: lifecycle,
		}
	}

	return PatchOperation{
		Op:    "add",
		Path:  path,
		Value: lifecycle,
	}
}

func addEnvVar(pod *corev1.Pod, containerIdx int, env corev1.EnvVar) PatchOperation {
	path := fmt.Sprintf("/spec/containers/%d/env", containerIdx)
	if len(pod.Spec.Containers[containerIdx].Env) == 0 {
		return PatchOperation{
			Op:    "add",
			Path:  path,
			Value: []corev1.EnvVar{env},
		}
	}
	return PatchOperation{
		Op:    "add",
		Path:  path + "/-",
		Value: env,
	}
}

func buildEnvVars(cfg *Config) []corev1.EnvVar {
	vars := []corev1.EnvVar{
		{Name: "PMS_IMAGE", Value: cfg.PMSImage},
		{Name: "TRANSCODE_PVC", Value: cfg.TranscodePVC},
		{
			Name: "KUBE_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}

	if cfg.DataPVC != "" {
		vars = append(vars, corev1.EnvVar{Name: "DATA_PVC", Value: cfg.DataPVC})
	}
	if cfg.ConfigPVC != "" {
		vars = append(vars, corev1.EnvVar{Name: "CONFIG_PVC", Value: cfg.ConfigPVC})
	}

	// Build PMS internal address
	pmsAddress := cfg.PMSService
	if pmsAddress != "" {
		if !strings.Contains(pmsAddress, "://") {
			pmsAddress = fmt.Sprintf("http://%s.%s.svc:32400", cfg.PMSService, cfg.Namespace)
		}
		vars = append(vars, corev1.EnvVar{Name: "PMS_INTERNAL_ADDRESS", Value: pmsAddress})
	}

	return vars
}
