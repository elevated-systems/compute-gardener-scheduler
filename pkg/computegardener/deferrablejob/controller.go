package deferrablejob

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// DeferrableJobReconciler reconciles a DeferrableJob object
type DeferrableJobReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=scheduling.x-k8s.io,resources=deferrablejobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=scheduling.x-k8s.io,resources=deferrablejobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=scheduling.x-k8s.io,resources=deferrablejobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DeferrableJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("deferrablejob", req.NamespacedName)

	var deferrableJob deferrablejobv1.DeferrableJob
	if err := r.Get(ctx, req.NamespacedName, &deferrableJob); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			log.Info("DeferrableJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "unable to fetch DeferrableJob")
		return ctrl.Result{}, err
	}

	cronJobName := fmt.Sprintf("deferrablejob-%s", deferrableJob.Name)
	var cronJob batchv1.CronJob
	err := r.Get(ctx, client.ObjectKey{Name: cronJobName, Namespace: deferrableJob.Namespace}, &cronJob)
	if err != nil && errors.IsNotFound(err) {
		cronJob = batchv1.CronJob{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cronJobName,
				Namespace: deferrableJob.Namespace,
			},
			Spec: batchv1.CronJobSpec{
				Schedule:          deferrableJob.Spec.Schedule,
				ConcurrencyPolicy: batchv1.ForbidConcurrent,
				JobTemplate: batchv1.JobTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": cronJobName},
					},
					Spec: corev1.JobSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								RestartPolicy: corev1.RestartPolicyOnFailure,
								Containers: []corev1.Container{
									{
										Name:  "deferrablejob",
										Image: "your-image-here", // Replace with the actual image
										Args:  []string{"run"},
									},
								},
							},
						},
					},
				},
			},
		}
		if err := ctrl.SetControllerReference(&deferrableJob, &cronJob, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Creating a new CronJob", "CronJob.Namespace", cronJob.Namespace, "CronJob.Name", cronJob.Name)
		err = r.Create(ctx, &cronJob)
		if err != nil {
			log.Error(err, "unable to create new CronJob", "CronJob.Namespace", cronJob.Namespace, "CronJob.Name", cronJob.Name)
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "unable to fetch CronJob")
		return ctrl.Result{}, err
	}

	if cronJob.Spec.Schedule != deferrableJob.Spec.Schedule {
		cronJob.Spec.Schedule = deferrableJob.Spec.Schedule
		err := r.Update(ctx, &cronJob)
		if err != nil {
			log.Error(err, "unable to update CronJob", "CronJob.Namespace", cronJob.Namespace, "CronJob.Name", cronJob.Name)
			return ctrl.Result{}, err
		}
	}

	deferrableJob.Status.LastScheduledTime = metav1.Time{Time: time.Now()}
	err = r.Status().Update(ctx, &deferrableJob)
	if err != nil {
		log.Error(err, "unable to update DeferrableJob status", "DeferrableJob.Namespace", deferrableJob.Namespace, "DeferrableJob.Name", deferrableJob.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeferrableJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deferrablejobv1.DeferrableJob{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
