/*

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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dioscuriv1 "github.com/amazeeio/dioscuri/api/v1"
	"github.com/go-logr/logr"
	certv1alpha2 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	"gopkg.in/matryer/try.v1"
	corev1 "k8s.io/api/core/v1"
	networkv1 "k8s.io/api/networking/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IngressMigrateReconciler reconciles a Migrate object
type IngressMigrateReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Labels map[string]string
}

// MigratedIngress .
type MigratedIngress struct {
	NewIngress          *networkv1.Ingress
	OldIngressNamespace string
}

// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=ingressmigrates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dioscuri.amazee.io,resources=ingressmigrates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="*",resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="*",resources=ingress/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="*",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="*",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="*",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="cert-manager.io",resources=certificates,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the actual reconcilation process
func (r *IngressMigrateReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	opLog := r.Log.WithValues("ingressmigrate", req.NamespacedName)
	// your logic here
	var dioscuri dioscuriv1.IngressMigrate
	if err := r.Get(ctx, req.NamespacedName, &dioscuri); err != nil {
		return ctrl.Result{}, IgnoreNotFound(err)
	}
	// your logic here
	finalizerName := "finalizer.dioscuri.amazee.io/v1"

	labels := map[string]string{
		LabelAppName:    dioscuri.ObjectMeta.Name,
		LabelAppType:    "dioscuri",
		LabelAppManaged: "ingress-migration",
	}
	r.Labels = labels

	// examine DeletionTimestamp to determine if object is under deletion
	if dioscuri.ObjectMeta.DeletionTimestamp.IsZero() {
		// check if the migrate annotation is set to true
		if dioscuri.Annotations["dioscuri.amazee.io/migrate"] == "true" {
			var activeMigratedIngress []string
			var standbyMigratedIngress []string
			// first set the migration to false after we start
			// this way we shouldn't proceed to do any more changes if there is an error as there might be something wrong further down.
			mergePatch, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"dioscuri.amazee.io/migrate": "false",
					},
				},
			})
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("Unable to create mergepatch for %s, error was: %v", dioscuri.ObjectMeta.Name, err)
			}
			if err := r.Patch(ctx,
				&dioscuri,
				client.ConstantPatch(
					types.MergePatchType,
					mergePatch,
				),
			); err != nil {
				return ctrl.Result{}, fmt.Errorf("Unable to patch ingressmigrate %s, error was: %v", dioscuri.ObjectMeta.Name, err)
			}
			r.updateStatusCondition(ctx,
				&dioscuri,
				dioscuriv1.IngressMigrateConditions{
					Status:    "True",
					Type:      "started",
					Condition: "Started ingress migration",
				},
				activeMigratedIngress,
				standbyMigratedIngress,
			)
			sourceNamespace := dioscuri.ObjectMeta.Namespace
			destinationNamespace := dioscuri.Spec.DestinationNamespace
			opLog.Info(fmt.Sprintf("Beginning ingress migration checks for ingress in %s moving to %s", sourceNamespace, destinationNamespace))

			// check destination namespace exists
			namespace := corev1.Namespace{}
			if err := r.Get(ctx,
				types.NamespacedName{
					Name: destinationNamespace,
				},
				&namespace,
			); err != nil {
				opLog.Info(fmt.Sprintf("Unable to find destination namespace, error was: %v", err))
				return ctrl.Result{}, nil
			}

			/*
				// START ACME-CHALLENGE CLEANUP SECTION
				// check ingress in the source namespace for any pending acme-challenges
				// check if any ingress in the source namespace have an exposer label, we need to remove these before we move any ingress
				acmeLabels := map[string]string{"acme.openshift.io/exposer": "true"}
				acmeSourceToDestination := &networkv1.IngressList{}
				if err := r.getIngressWithLabel(&dioscuri, acmeSourceToDestination, sourceNamespace, acmeLabels); err != nil {
					opLog.Info(fmt.Sprintf("%v", err))
				}
				if err := r.cleanUpAcmeChallenges(&dioscuri, acmeSourceToDestination); err != nil {
					r.updateStatusCondition(&dioscuri, dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					}, activeMigratedIngress, standbyMigratedIngress)
					return ctrl.Result{}, err
				}
				acmeDestinationToSource := &networkv1.IngressList{}
				if err := r.getIngressWithLabel(&dioscuri, acmeDestinationToSource, destinationNamespace, acmeLabels); err != nil {
					opLog.Info(fmt.Sprintf("%v", err))
				}
				if err := r.cleanUpAcmeChallenges(&dioscuri, acmeDestinationToSource); err != nil {
					r.updateStatusCondition(&dioscuri, dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					}, activeMigratedIngress, standbyMigratedIngress)
					return ctrl.Result{}, err
				}
				// END ACME-CHALLENGE CLEANUP SECTION
			*/
			// START CHECKING SERVICES SECTION
			migrateLabels := map[string]string{"dioscuri.amazee.io/migrate": "true"}
			// get the ingress from the source namespace, these will get moved to the destination namespace
			ingressSourceToDestination := &networkv1.IngressList{}
			if err := r.getIngressWithLabel(&dioscuri,
				ingressSourceToDestination,
				sourceNamespace,
				migrateLabels,
			); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			// get the ingress from the destination namespace, these will get moved to the source namespace
			ingressDestinationToSource := &networkv1.IngressList{}
			if err := r.getIngressWithLabel(&dioscuri,
				ingressDestinationToSource,
				destinationNamespace,
				migrateLabels,
			); err != nil {
				opLog.Info(fmt.Sprintf("%v", err))
			}
			// check that the services for the ingress we are moving exist in each namespace
			migrateSourceToDestination := &networkv1.IngressList{}
			if err := r.checkServices(ctx,
				&dioscuri,
				ingressSourceToDestination,
				migrateSourceToDestination,
				destinationNamespace,
			); err != nil {
				r.updateStatusCondition(ctx,
					&dioscuri,
					dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					},
					activeMigratedIngress,
					standbyMigratedIngress,
				)
				return ctrl.Result{}, nil
			}
			migrateDestinationToSource := &networkv1.IngressList{}
			if err := r.checkServices(ctx,
				&dioscuri,
				ingressDestinationToSource,
				migrateDestinationToSource,
				sourceNamespace,
			); err != nil {
				r.updateStatusCondition(ctx,
					&dioscuri,
					dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					},
					activeMigratedIngress,
					standbyMigratedIngress,
				)
				return ctrl.Result{}, nil
			}
			// check that the secrets for the ingress we are moving don't already exist in each namespace
			if err := r.checkSecrets(ctx,
				&dioscuri,
				ingressSourceToDestination,
				destinationNamespace,
			); err != nil {
				r.updateStatusCondition(ctx,
					&dioscuri,
					dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					},
					activeMigratedIngress,
					standbyMigratedIngress,
				)
				return ctrl.Result{}, nil
			}
			if err := r.checkSecrets(ctx,
				&dioscuri,
				ingressDestinationToSource,
				sourceNamespace,
			); err != nil {
				r.updateStatusCondition(ctx,
					&dioscuri,
					dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					},
					activeMigratedIngress,
					standbyMigratedIngress,
				)
				return ctrl.Result{}, nil
			}
			// END CHECKING SERVICES SECTION

			// START MIGRATING ROUTES SECTION
			// actually start the migrations here
			var migratedIngress []MigratedIngress
			for _, ingress := range migrateSourceToDestination.Items {
				// before we move anything we may need to modify some annotations
				// patch all the annotations we are given in the `pre-migrate-resource-annotations`
				// with the provided values
				if err := r.migrateResourcePatch(ctx,
					ingress,
					dioscuri.ObjectMeta.Annotations["dioscuri.amazee.io/pre-migrate-resource-annotations"],
				); err != nil {
					return ctrl.Result{}, err
				}

				// migrate these ingress
				newIngress, err := r.individualIngressMigration(ctx,
					&dioscuri,
					&ingress,
					sourceNamespace,
					destinationNamespace,
				)
				if err != nil {
					r.updateStatusCondition(ctx,
						&dioscuri,
						dioscuriv1.IngressMigrateConditions{
							Status:    "True",
							Type:      "failed",
							Condition: fmt.Sprintf("%v", err),
						},
						activeMigratedIngress,
						standbyMigratedIngress,
					)
					return ctrl.Result{}, err
				}
				migratedIngress = append(migratedIngress,
					MigratedIngress{
						NewIngress:          newIngress,
						OldIngressNamespace: sourceNamespace,
					},
				)
				ingressScheme := "http://"
				if ingress.Spec.TLS != nil {
					ingressScheme = "https://"
				}
				for _, rule := range ingress.Spec.Rules {
					activeMigratedIngress = append(activeMigratedIngress, fmt.Sprintf("%s%s", ingressScheme, rule.Host))
				}
			}
			for _, ingress := range migrateDestinationToSource.Items {
				// before we move anything we may need to modify some annotations
				// patch all the annotations we are given in the `pre-migrate-resource-annotations`
				// with the provided values
				if err := r.migrateResourcePatch(ctx,
					ingress,
					dioscuri.ObjectMeta.Annotations["dioscuri.amazee.io/pre-migrate-resource-annotations"],
				); err != nil {
					return ctrl.Result{}, err
				}
				// migrate these ingress
				newIngress, err := r.individualIngressMigration(ctx,
					&dioscuri,
					&ingress,
					destinationNamespace,
					sourceNamespace,
				)
				if err != nil {
					r.updateStatusCondition(ctx,
						&dioscuri,
						dioscuriv1.IngressMigrateConditions{
							Status:    "True",
							Type:      "failed",
							Condition: fmt.Sprintf("%v", err),
						},
						activeMigratedIngress,
						standbyMigratedIngress,
					)
					return ctrl.Result{}, err
				}
				// add the migrated ingress so we go through and fix them up later
				migratedIngress = append(migratedIngress,
					MigratedIngress{NewIngress: newIngress,
						OldIngressNamespace: destinationNamespace,
					},
				)
				ingressScheme := "http://"
				if ingress.Spec.TLS != nil {
					ingressScheme = "https://"
				}
				for _, rule := range ingress.Spec.Rules {
					standbyMigratedIngress = append(standbyMigratedIngress, fmt.Sprintf("%s%s", ingressScheme, rule.Host))
				}
			}
			// wait a sec before updating the ingress
			checkInterval := time.Duration(1)
			time.Sleep(checkInterval * time.Second)
			// once we move all the ingress, we have to go through and do a final update on them to make sure any `HostAlreadyClaimed` warning/errors go away
			for _, migratedIngress := range migratedIngress {
				// // we may need to move some resources after we move the ingress, we can define their annotations here

				err := r.updateIngress(ctx,
					&dioscuri,
					migratedIngress.NewIngress,
					migratedIngress.OldIngressNamespace,
					dioscuri.ObjectMeta.Annotations["dioscuri.amazee.io/post-migrate-resource-annotations"],
				)
				if err != nil {
					r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.IngressMigrateConditions{
						Status:    "True",
						Type:      "failed",
						Condition: fmt.Sprintf("%v", err),
					}, activeMigratedIngress, standbyMigratedIngress)
					return ctrl.Result{}, err
				}
			}

			r.updateStatusCondition(ctx, &dioscuri, dioscuriv1.IngressMigrateConditions{
				Status:    "True",
				Type:      "completed",
				Condition: "Completed ingress migration",
			}, activeMigratedIngress, standbyMigratedIngress)
		}
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !ContainsString(dioscuri.ObjectMeta.Finalizers, finalizerName) {
			dioscuri.ObjectMeta.Finalizers = append(dioscuri.ObjectMeta.Finalizers, finalizerName)
			mergePatch, _ := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"finalizers": dioscuri.ObjectMeta.Finalizers,
				},
			})
			if err := r.Patch(ctx, &dioscuri, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if ContainsString(dioscuri.ObjectMeta.Finalizers, finalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(&dioscuri, req.NamespacedName.Namespace); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}
			// remove our finalizer from the list and update it.
			dioscuri.ObjectMeta.Finalizers = RemoveString(dioscuri.ObjectMeta.Finalizers, finalizerName)
			mergePatch, _ := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"finalizers": dioscuri.ObjectMeta.Finalizers,
				},
			})
			if err := r.Patch(ctx, &dioscuri, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager setups the controller with a manager
func (r *IngressMigrateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dioscuriv1.IngressMigrate{}).
		Complete(r)
}

func (r *IngressMigrateReconciler) deleteExternalResources(dioscuri *dioscuriv1.IngressMigrate, namespace string) error {
	// delete any external resources associated with the autoidler
	return nil
}

func (r *IngressMigrateReconciler) checkServices(ctx context.Context,
	dioscuri *dioscuriv1.IngressMigrate,
	ingressList *networkv1.IngressList,
	ingressToMigrate *networkv1.IngressList,
	destinationNamespace string,
) error {
	// check service for ingress exists in destination namespace
	for _, ingress := range ingressList.Items {
		for _, host := range ingress.Spec.Rules {
			for _, path := range host.HTTP.Paths {
				service := &corev1.Service{}
				err := r.Get(ctx,
					types.NamespacedName{
						Namespace: destinationNamespace,
						Name:      path.Backend.ServiceName,
					},
					service,
				)
				if err != nil {
					if apierrors.IsNotFound(err) {
						return fmt.Errorf("Service %s for ingress %s doesn't exist in namespace %s, skipping ingress", path.Backend.ServiceName, host.Host, destinationNamespace)
					}
					return fmt.Errorf("Error getting service, error was: %v", err)
				}
				ingressToMigrate.Items = append(ingressToMigrate.Items, ingress)
			}
		}
	}
	return nil
}

func (r *IngressMigrateReconciler) checkSecrets(ctx context.Context,
	dioscuri *dioscuriv1.IngressMigrate,
	ingressList *networkv1.IngressList,
	destinationNamespace string,
) error {
	// check service for ingress exists in destination namespace
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	for _, ingress := range ingressList.Items {
		for _, hosts := range ingress.Spec.TLS {
			secret := &corev1.Secret{}
			err := r.Get(ctx,
				types.NamespacedName{
					Namespace: destinationNamespace,
					Name:      hosts.SecretName,
				},
				secret,
			)
			if err != nil {
				if apierrors.IsNotFound(err) {
					opLog.Info(fmt.Sprintf("Secret %s for ingress %s doesn't exist in namespace %s", hosts.SecretName, hosts, destinationNamespace))
					return nil
				}
				return fmt.Errorf("Error getting secret, error was: %v", err)
			}
			return fmt.Errorf("Secret %s for ingress %s exists in namespace %s, skipping ingress", hosts.SecretName, hosts, destinationNamespace)
		}
	}
	return nil
}

func (r *IngressMigrateReconciler) getIngressWithLabel(dioscuri *dioscuriv1.IngressMigrate,
	ingress *networkv1.IngressList,
	namespace string,
	labels map[string]string,
) error {
	// collect any ingress with specific labels
	listOption := (&client.ListOptions{}).ApplyOptions([]client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	})
	if err := r.List(context.TODO(), ingress, listOption); err != nil {
		return fmt.Errorf("Unable to get any ingress: %v", err)
	}
	return nil
}

func (r *IngressMigrateReconciler) cleanUpAcmeChallenges(dioscuri *dioscuriv1.IngressMigrate, ingressList *networkv1.IngressList) error {
	// we need to ensure there are no stale or pending acme challenges for any ingress we are going to
	// opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	// for _, ingress := range ingressList.Items {
	// 	opLog.Info(fmt.Sprintf("Found acme-challenge for %s, proceeding to delete the pending challenge before moving the ingress", ingress.Spec.Host))
	// 	// deep copy the ingress
	// 	acmeIngress := &networkv1.Ingress{}
	// 	ingress.DeepCopyInto(acmeIngress)
	// 	if err := r.removeIngress(acmeIngress); err != nil {
	// 		// we should break here before we try to migrate the ingress, broken acme is better than broken site
	// 		return fmt.Errorf("Unable to acme-challenge ingress %s in %s: %v", acmeIngress.ObjectMeta.Name, acmeIngress.ObjectMeta.Namespace, err)
	// 	}
	// }
	return nil
}

func (r *IngressMigrateReconciler) individualIngressMigration(ctx context.Context,
	dioscuri *dioscuriv1.IngressMigrate,
	ingress *networkv1.Ingress,
	sourceNamespace string,
	destinationNamespace string,
) (*networkv1.Ingress, error) {
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	oldIngress := &networkv1.Ingress{}
	newIngress := &networkv1.Ingress{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: sourceNamespace, Name: ingress.ObjectMeta.Name}, oldIngress)
	if err != nil {
		return newIngress, fmt.Errorf("Ingress %s in namespace %s doesn't exist: %v", ingress.ObjectMeta.Name, sourceNamespace, err)
	}
	ingressSecrets := r.copySecrets(ctx, oldIngress)
	if err := r.createSecrets(ctx, destinationNamespace, ingressSecrets); err != nil {
		return newIngress, fmt.Errorf("Unable to create secrets in destination namespace, error was: %v", err)
	}
	ingressCerts := r.copyCertificates(ctx, oldIngress)
	if err := r.createCertificates(ctx, destinationNamespace, ingressCerts); err != nil {
		return newIngress, fmt.Errorf("Unable to create secrets in destination namespace, error was: %v", err)
	}
	// if we ever need to do anything for any ingress with `tls-acme: true` enabled on them, for now, info only
	// if oldIngress.Annotations["kubernetes.io/tls-acme"] == "true" {
	// 	opLog.Info(fmt.Sprintf("Lets Encrypt is enabled for %s", oldIngress.Spec.Host))
	// }
	// actually migrate here
	// we need to create a new ingress now, but we need to swap the namespace to the destination.
	// deepcopyinto from old to the new ingress
	oldIngress.DeepCopyInto(newIngress)
	// set the newingress namespace as the destination namespace
	newIngress.ObjectMeta.Namespace = destinationNamespace
	newIngress.ObjectMeta.ResourceVersion = ""
	// opLog.Info(fmt.Sprintf("Attempting to migrate ingress %s - %s", newIngress.ObjectMeta.Name, newIngress.Spec.Host))
	if err := r.migrateIngress(ctx, dioscuri, newIngress, oldIngress); err != nil {
		return newIngress, fmt.Errorf("Error migrating ingress %s in namespace %s: %v", ingress.ObjectMeta.Name, sourceNamespace, err)
	}
	opLog.Info(fmt.Sprintf("Done migrating ingress %s", ingress.ObjectMeta.Name))
	return newIngress, nil
}

// add ingress, and then remove the old one only if we successfully create the new one
func (r *IngressMigrateReconciler) migrateIngress(ctx context.Context,
	dioscuri *dioscuriv1.IngressMigrate,
	newIngress *networkv1.Ingress,
	oldIngress *networkv1.Ingress,
) error {
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	// delete old ingress from the old namespace
	opLog.Info(fmt.Sprintf("Removing old ingress %s in namespace %s", oldIngress.ObjectMeta.Name, oldIngress.ObjectMeta.Namespace))
	if err := r.removeIngress(ctx, oldIngress); err != nil {
		return err
	}
	// add ingress
	if err := r.addIngressIfNotExist(ctx, dioscuri, newIngress); err != nil {
		return fmt.Errorf("Unable to create ingress %s in %s: %v", newIngress.ObjectMeta.Name, newIngress.ObjectMeta.Namespace, err)
	}
	return nil
}

func (r *IngressMigrateReconciler) updateIngress(ctx context.Context, dioscuri *dioscuriv1.IngressMigrate, newIngress *networkv1.Ingress, oldIngressNamespace string, postMigrateResourcesJSON string) error {
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	// check a few times to make sure the old ingress no longer exists
	for i := 0; i < 10; i++ {
		oldIngressExists := r.checkOldIngressExists(dioscuri, newIngress, oldIngressNamespace)
		if !oldIngressExists {
			// the new ingress with label to ensure ingresscontroller picks it up after we do the deletion
			mergePatch, err := json.Marshal(map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"dioscuri.amazee.io/migrated-from": oldIngressNamespace,
					},
				},
			})
			if err != nil {
				return fmt.Errorf("Unable to create mergepatch for %s, error was: %v", newIngress.ObjectMeta.Name, err)
			}
			deleted, secrets := r.deleteOldSecrets(ctx, oldIngressNamespace, newIngress)
			if !deleted {
				// there was an issue with some of the secrets remaining in the source namespace
				opLog.Info(fmt.Sprintf("The following secrets remained in the source namespace: %s", strings.Join(secrets, ",")))
			}
			opLog.Info(fmt.Sprintf("Patching ingress %s in namespace %s", newIngress.ObjectMeta.Name, newIngress.ObjectMeta.Namespace))
			if err := r.Patch(ctx, newIngress, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
				return fmt.Errorf("Unable to patch ingress %s, error was: %v", newIngress.ObjectMeta.Name, err)
			}
			if err := r.migrateResourcePatch(ctx, *newIngress, postMigrateResourcesJSON); err != nil {
				return err
			}
			return nil
		}
		// wait 5 secs before re-trying
		checkInterval := time.Duration(5)
		time.Sleep(checkInterval * time.Second)
	}
	return fmt.Errorf("There was an error checking if the old ingress still exists before trying to patch the new ingress, there may be an issue with the ingress")
}

// add any ingress if they don't already exist in the new namespace
func (r *IngressMigrateReconciler) addIngressIfNotExist(ctx context.Context, dioscuri *dioscuriv1.IngressMigrate, ingress *networkv1.Ingress) error {
	// add ingress
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	opLog.Info(fmt.Sprintf("Getting existing ingress %s in namespace %s", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace))
	err := r.Get(ctx, types.NamespacedName{Namespace: ingress.ObjectMeta.Namespace, Name: ingress.ObjectMeta.Name}, ingress)
	if err != nil {
		// there is no ingress in the destination namespace, then we create it
		opLog.Info(fmt.Sprintf("Creating ingress %s in namespace %s", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace))
		if err := r.Create(ctx, ingress); err != nil {
			return fmt.Errorf("Unable to create ingress %s in %s: %v", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace, err)
		}
	}
	return nil
}

func (r *IngressMigrateReconciler) checkOldIngressExists(dioscuri *dioscuriv1.IngressMigrate, ingress *networkv1.Ingress, sourceNamespace string) bool {
	opLog := r.Log.WithValues("ingressmigrate", dioscuri.ObjectMeta.Namespace)
	opLog.Info(fmt.Sprintf("Checking ingress %s is not in source namespace %s", ingress.ObjectMeta.Name, sourceNamespace))
	getIngress := &networkv1.Ingress{}
	err := r.Get(context.TODO(), types.NamespacedName{Namespace: sourceNamespace, Name: ingress.ObjectMeta.Name}, getIngress)
	if err != nil {
		// there is no ingress in the source namespace
		opLog.Info(fmt.Sprintf("Ingress %s is not in source namespace %s", ingress.ObjectMeta.Name, sourceNamespace))
		return false
	}
	opLog.Info(fmt.Sprintf("Ingress %s is in source namespace %s", ingress.ObjectMeta.Name, sourceNamespace))
	return true
}

// remove a given ingress
func (r *IngressMigrateReconciler) removeIngress(ctx context.Context, ingress *networkv1.Ingress) error {
	opLog := r.Log.WithValues("ingressmigrate", ingress.ObjectMeta.Namespace)
	// remove ingress
	if err := r.Delete(ctx, ingress); err != nil {
		return fmt.Errorf("Unable to delete ingress %s in %s: %v", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace, err)
	}
	// check that the ingress is actually deleted before continuing
	opLog.Info(fmt.Sprintf("Check ingress %s in %s deleted", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace))
	try.MaxRetries = 60
	err := try.Do(func(attempt int) (bool, error) {
		var ingressErr error
		err := r.Get(ctx, types.NamespacedName{
			Namespace: ingress.ObjectMeta.Namespace,
			Name:      ingress.ObjectMeta.Name,
		}, ingress)
		if err != nil {
			// the ingress doesn't exist anymore, so exit the retry
			ingressErr = nil
			opLog.Info(fmt.Sprintf("Ingress %s in %s deleted", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace))
		} else {
			// if the ingress still exists wait 5 seconds before trying again
			msg := fmt.Sprintf("Ingress %s in %s still exists", ingress.ObjectMeta.Name, ingress.ObjectMeta.Namespace)
			ingressErr = fmt.Errorf("%s: %v", msg, err)
			opLog.Info(msg)
		}
		time.Sleep(1 * time.Second)
		return attempt < 60, ingressErr
	})
	if err != nil {
		return err
	}
	return nil
}

// update status
func (r *IngressMigrateReconciler) updateStatusCondition(ctx context.Context,
	dioscuri *dioscuriv1.IngressMigrate,
	condition dioscuriv1.IngressMigrateConditions,
	activeIngress []string,
	standbyIngress []string,
) error {
	// set the transition time
	condition.LastTransitionTime = time.Now().UTC().Format(time.RFC3339)
	if !IngressContainsStatus(dioscuri.Status.Conditions, condition) {
		dioscuri.Status.Conditions = append(dioscuri.Status.Conditions, condition)
		mergePatch, _ := json.Marshal(map[string]interface{}{
			"status": map[string]interface{}{
				"conditions": dioscuri.Status.Conditions,
			},
			"spec": map[string]interface{}{
				"ingress": map[string]string{
					"activeIngress":  strings.Join(activeIngress, ","),
					"standbyIngress": strings.Join(standbyIngress, ","),
				},
			},
		})
		if err := r.Patch(ctx, dioscuri, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
			return fmt.Errorf("Unable to update status condition: %v", err)
		}
	}
	return nil
}

func (r *IngressMigrateReconciler) migrateResourcePatch(ctx context.Context, ingress networkv1.Ingress, migrateResourcesJSON string) error {
	if migrateResourcesJSON != "" {
		var migrateResources []map[string]interface{}
		migrateResourcesAnnotations := make(map[string]interface{})
		if err := json.Unmarshal([]byte(migrateResourcesJSON), &migrateResources); err != nil {
			panic(err)
		}
		for _, resource := range migrateResources {
			migrateResourcesAnnotations[fmt.Sprintf("%s", resource["name"])] = fmt.Sprintf("%s", resource["value"])
		}
		for _, tls := range ingress.Spec.TLS {
			certificate := certv1alpha2.Certificate{}
			err := r.Get(ctx, types.NamespacedName{Namespace: ingress.ObjectMeta.Namespace, Name: tls.SecretName}, &certificate)
			if err != nil {
				return fmt.Errorf("Unable to get certificate, error was: %v", err)
			}
			if err := r.patchCertificate(ctx, &certificate, migrateResourcesAnnotations); err != nil {
				return fmt.Errorf("Unable to patch certificate, error was: %v", err)
			}
		}
		for _, tls := range ingress.Spec.TLS {
			secret := corev1.Secret{}
			err := r.Get(ctx, types.NamespacedName{Namespace: ingress.ObjectMeta.Namespace, Name: tls.SecretName}, &secret)
			if err != nil {
				return fmt.Errorf("Unable to get secret, error was: %v", err)
			}
			if err := r.patchSecret(ctx, &secret, migrateResourcesAnnotations); err != nil {
				return fmt.Errorf("Unable to patch secret, error was: %v", err)
			}
		}
		if err := r.patchIngress(ctx, &ingress, migrateResourcesAnnotations); err != nil {
			return fmt.Errorf("Unable to patch ingress, error was: %v", err)
		}
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      ingress.ObjectMeta.Name,
			Namespace: ingress.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Patched ingress in namespace %s", ingress.ObjectMeta.Namespace))
		// fmt.Println(migrateResourcesAnnotations)
	}
	return nil
}

func (r *IngressMigrateReconciler) patchIngress(ctx context.Context, ingress *networkv1.Ingress, annotations map[string]interface{}) error {
	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	})
	if err != nil {
		return fmt.Errorf("Unable to create mergepatch for %s, error was: %v", ingress.ObjectMeta.Name, err)
	}
	if err := r.Patch(ctx, ingress, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
		return fmt.Errorf("Unable to patch ingress %s, error was: %v", ingress.ObjectMeta.Name, err)
	}
	r.Log.WithValues("ingress", types.NamespacedName{
		Name:      ingress.ObjectMeta.Name,
		Namespace: ingress.ObjectMeta.Namespace,
	}).Info(fmt.Sprintf("Patched ingress %s", ingress.ObjectMeta.Name))
	return nil
}

func (r *IngressMigrateReconciler) patchSecret(ctx context.Context, secret *corev1.Secret, annotations map[string]interface{}) error {
	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	})
	if err != nil {
		return fmt.Errorf("Unable to create mergepatch for %s, error was: %v", secret.ObjectMeta.Name, err)
	}
	if err := r.Patch(ctx, secret, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
		return fmt.Errorf("Unable to patch ingress %s, error was: %v", secret.ObjectMeta.Name, err)
	}
	r.Log.WithValues("ingress", types.NamespacedName{
		Name:      secret.ObjectMeta.Name,
		Namespace: secret.ObjectMeta.Namespace,
	}).Info(fmt.Sprintf("Patched secret %s", secret.ObjectMeta.Name))
	return nil
}

func (r *IngressMigrateReconciler) patchCertificate(ctx context.Context, certificate *certv1alpha2.Certificate, annotations map[string]interface{}) error {
	mergePatch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	})
	if err != nil {
		return fmt.Errorf("Unable to create mergepatch for %s, error was: %v", certificate.ObjectMeta.Name, err)
	}
	if err := r.Patch(ctx, certificate, client.ConstantPatch(types.MergePatchType, mergePatch)); err != nil {
		return fmt.Errorf("Unable to certificate ingress %s, error was: %v", certificate.ObjectMeta.Name, err)
	}
	r.Log.WithValues("ingress", types.NamespacedName{
		Name:      certificate.ObjectMeta.Name,
		Namespace: certificate.ObjectMeta.Namespace,
	}).Info(fmt.Sprintf("Patched certificate %s", certificate.ObjectMeta.Name))
	return nil
}

// copy any secret into a slice of secrets
func (r *IngressMigrateReconciler) copySecrets(ctx context.Context, ingress *networkv1.Ingress) []*corev1.Secret {
	var secrets []*corev1.Secret
	for _, tls := range ingress.Spec.TLS {
		secret := &corev1.Secret{}
		err := r.Get(ctx, types.NamespacedName{Namespace: ingress.ObjectMeta.Namespace, Name: tls.SecretName}, secret)
		if err != nil {
			break
		}
		secrets = append(secrets, secret)
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      secret.ObjectMeta.Name,
			Namespace: secret.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Copying secret %s in namespace %s", secret.ObjectMeta.Name, secret.ObjectMeta.Namespace))
	}
	return secrets
}

// create secret in destination namespace
func (r *IngressMigrateReconciler) createSecrets(ctx context.Context, destinationNamespace string, secrets []*corev1.Secret) error {
	for _, secret := range secrets {
		secret.ObjectMeta.Namespace = destinationNamespace
		secret.ResourceVersion = ""
		secret.SelfLink = ""
		secret.UID = ""
		err := r.Create(ctx, secret)
		if err != nil {
			break
		}
		secrets = append(secrets, secret)
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      secret.ObjectMeta.Name,
			Namespace: secret.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Creating secret %s in namespace %s", secret.ObjectMeta.Name, secret.ObjectMeta.Namespace))
	}
	return nil
}

// delete any old secrets in the namespace
func (r *IngressMigrateReconciler) deleteOldSecrets(ctx context.Context, namespace string, ingress *networkv1.Ingress) (bool, []string) {
	deleted := true
	var secrets []string
	for _, tls := range ingress.Spec.TLS {
		certificate := &certv1alpha2.Certificate{}
		err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: tls.SecretName}, certificate)
		if err != nil {
			secrets = append(secrets, tls.SecretName)
			deleted = false
		}
		if err = r.Delete(ctx, certificate); err != nil {
			r.Log.WithValues("ingress", types.NamespacedName{
				Name:      certificate.ObjectMeta.Name,
				Namespace: certificate.ObjectMeta.Namespace,
			}).Info(fmt.Sprintf("Unable to delete certificate %s in namespace %s; error was: %v", certificate.ObjectMeta.Name, certificate.ObjectMeta.Namespace, err))
		}
		secret := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: tls.SecretName}, secret)
		if err != nil {
			secrets = append(secrets, tls.SecretName)
			deleted = false
		}
		if err = r.Delete(ctx, secret); err != nil {
			r.Log.WithValues("ingress", types.NamespacedName{
				Name:      secret.ObjectMeta.Name,
				Namespace: secret.ObjectMeta.Namespace,
			}).Info(fmt.Sprintf("Unable to patch secret %s in namespace %s; error was: %v", secret.ObjectMeta.Name, secret.ObjectMeta.Namespace, err))
		}
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      secret.ObjectMeta.Name,
			Namespace: secret.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Added delete annotation to secret %s in namespace %s", secret.ObjectMeta.Name, secret.ObjectMeta.Namespace))

	}
	return deleted, secrets
}

// copy any certificate into a slice of certificates
func (r *IngressMigrateReconciler) copyCertificates(ctx context.Context, ingress *networkv1.Ingress) []*certv1alpha2.Certificate {
	var certificates []*certv1alpha2.Certificate
	for _, tls := range ingress.Spec.TLS {
		certificate := &certv1alpha2.Certificate{}
		err := r.Get(ctx, types.NamespacedName{Namespace: ingress.ObjectMeta.Namespace, Name: tls.SecretName}, certificate)
		if err != nil {
			break
		}
		certificates = append(certificates, certificate)
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      certificate.ObjectMeta.Name,
			Namespace: certificate.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Copying certificate %s in namespace %s", certificate.ObjectMeta.Name, certificate.ObjectMeta.Namespace))
	}
	return certificates
}

// create any certificates in the destination namespace
func (r *IngressMigrateReconciler) createCertificates(ctx context.Context, destinationNamespace string, certificates []*certv1alpha2.Certificate) error {
	for _, certificate := range certificates {
		certificate.ObjectMeta.Namespace = destinationNamespace
		certificate.ResourceVersion = ""
		certificate.SelfLink = ""
		certificate.UID = ""
		err := r.Create(ctx, certificate)
		if err != nil {
			break
		}
		// secrets = append(secrets, certificate)
		r.Log.WithValues("ingress", types.NamespacedName{
			Name:      certificate.ObjectMeta.Name,
			Namespace: certificate.ObjectMeta.Namespace,
		}).Info(fmt.Sprintf("Creating certificate %s in namespace %s", certificate.ObjectMeta.Name, certificate.ObjectMeta.Namespace))
	}
	return nil
}
