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

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	runnerv1alpha1 "github/devops/gitlab-runner-operator/api/v1alpha1"
)

// RunnerReconciler reconciles a Runner object
type RunnerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const requeueDelay = time.Second * 10

// +kubebuilder:rbac:groups=runner.gscout.xyz,resources=runners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=runner.gscout.xyz,resources=runners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=runner.gscout.xyz,resources=runners/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=list;watch;
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete;
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete;
func (r *RunnerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("runner", req.NamespacedName)
	runner := &runnerv1alpha1.Runner{}
	err := r.Get(ctx, req.NamespacedName, runner)

	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			log.Info("Runner resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get Runner")
		return ctrl.Result{}, err
	}

	tokenSecretErr := verifySecretCreated(ctx, r, runner.Spec.Credentials.TokenSecretName, runner.Namespace)
	certSecretErr := verifySecretCreated(ctx, r, runner.Spec.Credentials.CACertSecretName, runner.Namespace)
	imagePullSecretErr := verifySecretCreated(ctx, r, runner.Spec.RunnerServiceAccount.ImagePullSecretName, runner.Namespace)

	if tokenSecretErr != nil || certSecretErr != nil || imagePullSecretErr != nil {
		log.Info("Required secrets not available, requeueing. ")
		runner.Status.PodStatus = "Pending Secret Creation"
		runner.Status.IsRegistered = false
		err := r.Status().Update(ctx, runner)
		if err != nil {
			log.Error(err, "Failed to update Runner status")
			return ctrl.Result{RequeueAfter: requeueDelay}, err
		}
		return ctrl.Result{RequeueAfter: requeueDelay}, err
	}

	// Check if the configmap already exists, if not create a new one
	foundCM := &corev1.ConfigMap{}
	err = r.Get(ctx, types.NamespacedName{Name: "gitlab-runner-config", Namespace: runner.Namespace}, foundCM)
	if err != nil && errors.IsNotFound(err) {
		cm := r.constructConfigMapForRunner(runner)
		log.Info("Creating ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
		err = r.Create(ctx, cm)
		if err != nil {
			log.Error(err, "Failed to create ConfigMap", "ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get ConfigMap")
		return ctrl.Result{}, err
	}

	// Check if the serviceaccount already exists, if not create a new one
	foundSA := &corev1.ServiceAccount{}
	err = r.Get(ctx, types.NamespacedName{Name: runner.Spec.RunnerServiceAccount.Name, Namespace: runner.Namespace}, foundSA)
	if err != nil && errors.IsNotFound(err) {
		sa := r.constructServiceAccountForRunner(runner)
		log.Info("Creating ServiceAccount", "ServiceAccount.Namespace", sa.Namespace, "ServiceAccount.Name", sa.Name)
		err = r.Create(ctx, sa)
		if err != nil {
			log.Error(err, "Failed to create ServiceAccount", "ServiceAccount.Namespace", sa.Namespace, "ServiceAccount.Name", sa.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get ServiceAccount")
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	foundDep := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: runner.Name, Namespace: runner.Namespace}, foundDep)
	if err != nil && errors.IsNotFound(err) {
		dep := r.constructRunnerDeployment(runner)
		log.Info("Creating Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		err = r.Create(ctx, dep)
		if err != nil {
			log.Error(err, "Failed to create Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return ctrl.Result{RequeueAfter: requeueDelay}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Verifies whether a secret with a given name/namespace exists.
// Returns an error if the secret does not exist.
func verifySecretCreated(ctx context.Context, r *RunnerReconciler, secretName string, secretNamespace string) error {
	log := r.Log.WithValues("runner", secretNamespace)
	if secretName != "" {
		err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, &corev1.Secret{})
		if err != nil && errors.IsNotFound(err) {
			log.Info("Required secret not found.", "Secret", secretName)
			return err
		} else if err != nil {
			log.Error(err, "Failed to get secret")
			return err
		}
	}
	return nil
}

// Creates a GitLab runner ConfigMap and returns the runner ConfigMap object.
func (r *RunnerReconciler) constructConfigMapForRunner(m *runnerv1alpha1.Runner) *corev1.ConfigMap {

	cmData := map[string]string{
		"gitlab-server-address":      m.Spec.URL,
		"runner-tag-list":            strings.Join(m.Spec.TagList, ","),
		"kubernetes-namespace":       m.Namespace,
		"kubernetes-service-account": m.Spec.RunnerServiceAccount.Name,
		"allow_privilege_escalation": "false",
		"privileged":                 "false",
		"config.toml": "concurrent = " + fmt.Sprint(m.Spec.MaxConcurrentJobs) + "\n" +
			"log_format = \"json\"\n" +
			"check_interval = 3",
		"entrypoint": "set -xe\n" +
			"cp /tmp/config.toml /etc/gitlab-runner/config.toml\n" +
			"# Register the runner\n" +
			"/entrypoint register" +
			" --non-interactive" +
			" --url $GITLAB_SERVER_ADDRESS" +
			" --registration-token $REGISTRATION_TOKEN" +
			" --executor kubernetes\n" +
			"# Start the runner\n" +
			"/entrypoint run" +
			" --user=gitlab-runner" +
			" --working-directory=/home/gitlab-runner" +
			" --listen-address=:9252",
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gitlab-runner-config",
			Namespace: m.Namespace,
		},
		Data: cmData,
	}

	// Set Runner instance as the owner and controller
	ctrl.SetControllerReference(m, cm, r.Scheme)
	return cm
}

// Creates a GitLab runner ServiceAccount and returns the runner ServiceAccount object.
func (r *RunnerReconciler) constructServiceAccountForRunner(m *runnerv1alpha1.Runner) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Spec.RunnerServiceAccount.Name,
			Namespace: m.Namespace,
		},
	}

	// If a workload identity service account name was provided, apply the annotation
	// https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity
	if m.Spec.RunnerServiceAccount.WorkloadIdentityServiceAccount != "" {
		saAnnotations := map[string]string{
			"iam.gke.io/gcp-service-account": m.Spec.RunnerServiceAccount.WorkloadIdentityServiceAccount,
		}
		sa.ObjectMeta.Annotations = saAnnotations
	}

	// If an image pull secret name was provided, specify the configuration.
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#add-imagepullsecrets-to-a-service-account
	if m.Spec.RunnerServiceAccount.ImagePullSecretName != "" {
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{
			Name: m.Spec.RunnerServiceAccount.ImagePullSecretName,
		}}
	}

	// Set Runner instance as the owner and controller
	ctrl.SetControllerReference(m, sa, r.Scheme)
	return sa
}

func createVolumesFromCM(runner *runnerv1alpha1.Runner) *[]corev1.Volume {
	var customCertDefaultVolumeMode int32 = 420
	var customCertVolumeOptional bool = true
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "gitlab-runner-config"}}},
		},
	}

	if runner.Spec.Credentials.CACertSecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "custom-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					DefaultMode: &customCertDefaultVolumeMode,
					SecretName:  runner.Spec.Credentials.CACertSecretName,
					Optional:    &customCertVolumeOptional}}},
		)
	}
	return &volumes
}

func createVolumeMounts(runner *runnerv1alpha1.Runner) *[]corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "config",
			MountPath: "/scripts/entrypoint",
			ReadOnly:  true,
			SubPath:   "entrypoint"},
		{
			Name:      "config",
			MountPath: "/tmp/config.toml",
			SubPath:   "config.toml"},
	}

	if runner.Spec.Credentials.CACertSecretName != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "custom-certs",
			MountPath: "/etc/gitlab-runner/certs/",
			ReadOnly:  true},
		)
	}

	return &volumeMounts
}

func createEnvVariables(runner *runnerv1alpha1.Runner) *[]corev1.EnvVar {
	envVar := []corev1.EnvVar{
		{
			Name: "GITLAB_SERVER_ADDRESS",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "gitlab-runner-config"},
					Key: "gitlab-server-address"}}},
		{
			Name: "RUNNER_TAG_LIST",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "gitlab-runner-config"},
					Key: "runner-tag-list"}}},
		{
			Name: "KUBERNETES_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "gitlab-runner-config"},
					Key: "kubernetes-namespace"}}},
		{
			Name: "KUBERNETES_SERVICE_ACCOUNT",
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "gitlab-runner-config"},
					Key: "kubernetes-service-account"}}},
		{
			Name: "REGISTRATION_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: runner.Spec.Credentials.TokenSecretName},
					Key: "runner-registration-token"}}},
	}

	return &envVar
}

// Creates a GitLab runner Deployment and returns the runner Deployment object.
func (r *RunnerReconciler) constructRunnerDeployment(runner *runnerv1alpha1.Runner) *appsv1.Deployment {
	volumes := createVolumesFromCM(runner)
	volumeMounts := createVolumeMounts(runner)
	envVar := createEnvVariables(runner)
	labelSet := map[string]string{
		"runner-name": runner.Name,
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runner.Name,
			Namespace: runner.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labelSet,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labelSet,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "gitlab-runner",
					Containers: []corev1.Container{{
						Image:        "gcr.io/abm-test-bed/gitlab-runner:latest",
						Name:         "gitlab-runner",
						Command:      []string{"/bin/bash", "/scripts/entrypoint"},
						Env:          *envVar,
						VolumeMounts: *volumeMounts,
					}},
					Volumes: *volumes,
				}},
		},
	}

	ctrl.SetControllerReference(runner, dep, r.Scheme)
	return dep
}

// SetupWithManager sets up the controller with the Manager.
func (r *RunnerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&runnerv1alpha1.Runner{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
