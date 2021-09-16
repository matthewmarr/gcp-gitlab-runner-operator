/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RunnerSpec defines the desired state of Runner
type RunnerSpec struct {
	// +optional
	Resources corev1.ResourceRequirements `json:"resources"`

	// +optional
	// +kubebuilder:default=10
	MaxConcurrentJobs int32 `json:"maxConcurrentJobs"`

	Credentials RunnerCreds `json:"credentials"`

	// +optional
	// +kubebuilder:default="https://gitlab.com"
	URL string `json:"url"`

	// +optional
	// +kubebuilder:default="gitlab"
	JobNamespace string `json:"jobNamespace"`

	TagList []string `json:"tagList"`

	RunnerServiceAccount RunnerServiceAccount `json:"runnerServiceAccount"`
}

// RunnerServiceAccount defines k8s service account required values.
type RunnerServiceAccount struct {
	// +kubebuilder:default="gitlab-runner"
	Name string `json:"name"`

	// +optional
	WorkloadIdentityServiceAccount string `json:"workloadIdentityServiceAccount"`

	// +optional
	ImagePullSecretName string `json:"imagePullSecretName"`
}

// RunnerCreds defines the credential secret names required for the runner.
type RunnerCreds struct {
	// +optional
	// +kubebuilder:default="gitlab-runner-token"
	TokenSecretName string `json:"tokenSecretName"`

	// +optional
	// +kubebuilder:default=""
	CACertSecretName string `json:"cacertSecretName"`
}

// RunnerStatus defines the observed state of Runner
type RunnerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	IsRegistered bool   `json:"isRegistered"`
	PodStatus    string `json:"podStatus"`
}

// Runner is the Schema for the runners API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=".spec.url",name="URL",type="string"
// +kubebuilder:printcolumn:JSONPath=".spec.tagList",name="Tags",type="string"
// +kubebuilder:printcolumn:JSONPath=".status.isRegistered",name="Registered",type="boolean"
// +kubebuilder:printcolumn:JSONPath=".status.podStatus",name="Status",type="string"
type Runner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RunnerSpec   `json:"spec,omitempty"`
	Status RunnerStatus `json:"status,omitempty"`
}

// RunnerList contains a list of Runner
// +kubebuilder:object:root=true
type RunnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Runner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Runner{}, &RunnerList{})
}
