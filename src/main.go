package main

import (
	"fmt"
	"net/http"
	"io/ioutil"
	"net/http/httputil"
	"errors"
	"github.com/sirupsen/logrus"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/json"
)

var (
	logger *logrus.Logger
	universalDeserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()
)

type Config struct {
	Containers []corev1.Container `yaml:"containers"`
}

type patchOperation struct {
	Op    string      `json:"op"`  // Operation
	Path  string      `json:"path"` // Path
	Value interface{} `json:"value,omitempty"`
}


func init() {
	logger = logrus.New()
	logger.SetFormatter(
		&logrus.TextFormatter{
			DisableColors: true,
			FullTimestamp: true,
		})
	logger.SetLevel(logrus.TraceLevel)
}

func main() {

	logger.Info("Starting webhook server...")
	http.HandleFunc("/mutate", HandleMutate)
	logger.Fatal(http.ListenAndServeTLS(":8443", "/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key", nil))
}

func getAdmissionReviewRequest(w http.ResponseWriter, r *http.Request) admissionv1.AdmissionReview {
	requestDump, _ := httputil.DumpRequest(r, true)
	fmt.Printf("Request:\n%s\n", requestDump)

	// Grabbing the http body received on webhook.
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Panic("Error reading webhook request: ", err.Error())
	}

	// Required to pass to universal decoder.
	// v1beta1 also needs to be added to webhook.yaml
	var admissionReviewReq admissionv1.AdmissionReview

	logger.Info("deserializing admission review request")
	if _, _, err := universalDeserializer.Decode(body, nil, &admissionReviewReq); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		logger.Errorf("could not deserialize request: %v", err)
	} else if admissionReviewReq.Request == nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = errors.New("malformed admission review: request is nil")
	}
	return admissionReviewReq
}

func HandleMutate(w http.ResponseWriter, r *http.Request){
	// func getAdmissionReviewRequest, grab body from request, define AdmissionReview
	// and use universalDeserializer to decode body to admissionReviewReq
	admissionReviewReq := getAdmissionReviewRequest(w, r)
	uniqueId := string(admissionReviewReq.Request.UID)

	// Debug statement to verify if universalDeserializer worked
	logger.Infof("Type: %v, Event: %v, Id: %v", admissionReviewReq.Request.Kind, admissionReviewReq.Request.Operation, admissionReviewReq.Request.UID)

	// We now need to capture Pod object from the admission request
	var pod corev1.Pod
	err := json.Unmarshal(admissionReviewReq.Request.Object.Raw, &pod)
	if err != nil {
		logger.Errorf("could not unmarshal pod on admission request: %v", err)
	}

	// To perform a mutation on the object before the Kubernetes API sees the object, we can apply a patch to the operation
	var sideCarConfig *Config
	sideCarConfig = getSideCarConfig(uniqueId)
	patches, _ := createPatch(pod, sideCarConfig)

	// Once you have completed all patching, convert the patches to byte slice:
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		logger.Errorf("could not marshal JSON patch: %v", err)
	}

	// Add patchBytes to the admission response
	admissionReviewResponse := admissionv1.AdmissionReview{
		Response: &admissionv1.AdmissionResponse{
			UID: admissionReviewReq.Request.UID,
			Allowed: true,
		},
	}
	admissionReviewResponse.Response.Patch = patchBytes

	// Submit the response
	bytes, err := json.Marshal(&admissionReviewResponse)
	if err != nil {
		logger.Errorf("marshaling response: %v", err)
	}

	_, err = w.Write(bytes)
	if err != nil {
		return
	}
}

func getSideCarConfig(uniqueId string) *Config {
	var containers []corev1.Container

	logger.Debug("generating nginx side car config...")
	container := corev1.Container{
		Name: "sidecar",
		Image: "ubuntu",
		ImagePullPolicy: corev1.PullAlways,
		Command: []string{"sleep", "1d"},
	}

	sideCars := []corev1.Container{container}
	containers = sideCars

	return &Config{
		Containers: containers,
	}
}

func addContainer(target, containers []corev1.Container, basePath string) (patch []patchOperation) {
	first := len(target) == 0
	var value interface{}

	for _, add := range containers {
		value = add
		path := basePath
		if first {
			first = false
			value = []corev1.Container{add}
		} else {
			path = path + "/-"
		}
		logger.Debugf("container json patch Op: %s, Path: %s, Value: %+v", "add", path, value)
		patch = append(patch, patchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}

	return patch
}

func createPatch(pod corev1.Pod, sidecarConfig *Config) ([]patchOperation, error) {
	logger.Info("creating json patch of pod for sidecar config")
	var patches []patchOperation
	patches = append(patches, addContainer(pod.Spec.Containers, sidecarConfig.Containers, "/spec/containers")...)
	return patches, nil
}
