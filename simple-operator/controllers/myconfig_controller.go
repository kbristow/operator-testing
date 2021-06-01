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
	config_sdk "github.com/kbristow/simple-operator/config-sdk"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	simplev1alpha1 "github.com/kbristow/simple-operator/api/v1alpha1"
)

// MyConfigReconciler reconciles a MyConfig object
type MyConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=simple.absa.subatomic,resources=myconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=simple.absa.subatomic,resources=myconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=simple.absa.subatomic,resources=myconfigs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MyConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("myconfig", req.NamespacedName)

	myConfig := &simplev1alpha1.MyConfig{}
	err := r.Get(ctx, req.NamespacedName, myConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// This happens when the resource gets deleted. We can ignore this
			return ctrl.Result{}, nil
		} else {
			log.Error(err, "Unexpected error getting MyConfig resource")
			return ctrl.Result{}, err
		}
	}

	configService := config_sdk.NewConfigService()

	config, err := configService.GetConfig(myConfig.Spec.ConfigName)
	if err != nil {
		log.Error(err, "Failed to find the config definition for MyConfig resource")
		return ctrl.Result{}, err
	}

	configMapNamespaceName := types.NamespacedName{
		Namespace: myConfig.Namespace,
		Name:      myConfig.Spec.TargetConfigMap,
	}

	configMap := v1.ConfigMap{}

	err = r.Client.Get(ctx, configMapNamespaceName, &configMap)

	configMapExists := true

	if err != nil {
		if errors.IsNotFound(err) {
			configMapExists = false
		} else {
			log.Error(err, "Failed to find the config map for MyConfig resource")
			return ctrl.Result{}, err
		}
	}

	configMapUpdated := false

	if !configMapExists {
		configMapUpdated = true
		configMap = v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      myConfig.Spec.TargetConfigMap,
				Namespace: myConfig.Namespace,
			},
			Data: map[string]string{
				myConfig.Spec.ConfigName: config,
			},
		}
		_ = ctrl.SetControllerReference(myConfig, &configMap, r.Scheme)

		err := r.Client.Create(ctx, &configMap)
		if err != nil {
			log.Error(err, "Failed to create the config map for MyConfig resource")
			return ctrl.Result{}, err
		}
	} else {
		if configMap.Data[myConfig.Spec.ConfigName] != config {
			configMapUpdated = true
			log.Info("Config is out of date. Updating.")
			configMap.Data[myConfig.Spec.ConfigName] = config
			err := r.Client.Update(ctx, &configMap)
			if err != nil {
				log.Error(err, "Failed to update the config map for MyConfig resource")
				return ctrl.Result{}, err
			}
		}
	}

	if configMapUpdated {
		// reload the MyConfig in case it is now out of date
		err := r.Get(ctx, req.NamespacedName, myConfig)
		if err != nil {
			log.Error(err, "Failed to get the latest definition for MyConfig resource to update status")
			return ctrl.Result{}, err
		}
		myConfig.Status.Version += 1
		err = r.Client.Status().Update(ctx, myConfig)
		if err != nil {
			log.Error(err, "Failed to update MyConfig resource status")
			return ctrl.Result{}, err
		}
	}

	log.Info("Successfully reconciled MyConfig")

	return ctrl.Result{RequeueAfter: time.Duration(30) * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MyConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&simplev1alpha1.MyConfig{}).
		Complete(r)
}
