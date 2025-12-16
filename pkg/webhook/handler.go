package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
}

// Handler handles admission webhook requests
type Handler struct{}

// NewHandler creates a new webhook handler
func NewHandler() *Handler {
	return &Handler{}
}

// ServeHTTP handles the admission webhook HTTP request
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("error reading body: %v", err)
		http.Error(w, "could not read request body", http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		http.Error(w, "invalid content type, expected application/json", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *admissionv1.AdmissionResponse
	admissionReview := admissionv1.AdmissionReview{}

	if _, _, err := deserializer.Decode(body, nil, &admissionReview); err != nil {
		log.Printf("error decoding admission review: %v", err)
		admissionResponse = &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = h.mutate(&admissionReview)
	}

	responseReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	if admissionReview.Request != nil {
		responseReview.Response.UID = admissionReview.Request.UID
	}

	respBytes, err := json.Marshal(responseReview)
	if err != nil {
		log.Printf("error marshaling response: %v", err)
		http.Error(w, "could not marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		log.Printf("error writing response: %v", err)
	}
}

func (h *Handler) mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request
	if req == nil {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	log.Printf("AdmissionReview for Kind=%s, Namespace=%s, Name=%s, UID=%s",
		req.Kind.Kind, req.Namespace, req.Name, req.UID)

	// Only handle Pod resources
	if req.Kind.Kind != "Pod" {
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		log.Printf("error unmarshaling pod: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("could not unmarshal pod: %v", err),
			},
		}
	}

	// Check if mutation is enabled
	if !ShouldMutate(&pod) {
		log.Printf("skipping mutation for pod %s/%s (not enabled)", req.Namespace, pod.Name)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	log.Printf("mutating pod %s/%s", req.Namespace, pod.Name)

	// Extract configuration
	cfg, err := ExtractConfig(&pod, req.Namespace)
	if err != nil {
		log.Printf("error extracting config: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("configuration error: %v", err),
			},
		}
	}

	// Create patch
	patchBytes, err := CreatePatch(&pod, cfg)
	if err != nil {
		log.Printf("error creating patch: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: fmt.Sprintf("could not create patch: %v", err),
			},
		}
	}

	log.Printf("patch for pod %s/%s: %s", req.Namespace, pod.Name, string(patchBytes))

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}
}
