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
	runnerv1alpha1 "github/devops/gitlab-runner-operator/api/v1alpha1"
	"io"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// RunnerReconciler reconciles a Runner object
type PodReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=runner.gscout.xyz,resources=runners,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods;pods/log,verbs=get;list;watch;
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("pod", req.NamespacedName)
	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)

	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			log.Info("Pod resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get Pod")
		return ctrl.Result{}, err
	}

	newPodStatus := string(pod.Status.Phase)
	if newPodStatus != "Running" {
		log.Info("Waiting for pod " + req.Name + " to be running.")
		return ctrl.Result{}, nil
	}

	runner := &runnerv1alpha1.Runner{}
	runnerNamespacedName := types.NamespacedName{
		Name:      pod.GetLabels()["runner-name"],
		Namespace: req.Namespace,
	}
	err = r.Get(ctx, runnerNamespacedName, runner)

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

	if runner.Status.PodStatus != newPodStatus {
		runner.Status.PodStatus = string(pod.Status.Phase)
		err = r.Status().Update(ctx, runner)
		if err != nil {
			log.Error(err, "Failed to update Runner pod status")
			return ctrl.Result{}, err
		}
	}

	// Once isRegistered is flipped to true, we no longer need to check.
	if !runner.Status.IsRegistered {
		isRegistered, err := isRunnerRegistered(ctx, pod.Namespace, pod.Name, "gitlab-runner")
		if err != nil {
			log.Error(err, "Failed to fetch runner logs")
			return ctrl.Result{}, err
		} else {
			runner.Status.IsRegistered = isRegistered
			err := r.Status().Update(ctx, runner)
			if err != nil {
				log.Error(err, "Failed to update Runner status")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// Determines if the GitLab runner deployment was able to register to the GitLab server
// by parsing the pod logs for the string "Runner registered successfully".
func isRunnerRegistered(ctx context.Context, namespace string, podName string, containerName string) (isRegistered bool, err error) {
	count := int64(100)
	config, err := rest.InClusterConfig()

	if err != nil {
		return false, err
	}
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return false, err
	}

	// Ensure pod has enough time to register once it starts running.
	time.Sleep(5 * time.Second)

	podLogOptions := corev1.PodLogOptions{
		Container: containerName,
		Follow:    false,
		TailLines: &count,
	}

	podLogRequest := clientSet.CoreV1().
		Pods(namespace).
		GetLogs(podName, &podLogOptions)

	stream, err := podLogRequest.Stream(ctx)
	if err != nil {
		return false, err
	}
	defer stream.Close()

	for {
		buf := make([]byte, 2000)
		numBytes, err := stream.Read(buf)
		if numBytes == 0 {
			continue
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return false, err
		}

		message := buf[:numBytes]
		registrationRE := regexp.MustCompile(`Runner registered successfully`)
		isRegistered := registrationRE.Match(message)

		if isRegistered == true {
			return true, nil
		}
	}
	return false, nil
}

func ignoreNonRunnerPods() predicate.Predicate {
	// Filter all events that aren't updates on runner pods.
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if _, ok := e.ObjectNew.GetLabels()["runner-name"]; !ok {
				return false
			}
			return e.ObjectOld != e.ObjectNew
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(ignoreNonRunnerPods()).
		Complete(r)
}
