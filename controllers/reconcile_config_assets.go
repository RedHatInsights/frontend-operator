/*
Copyright 2025 RedHatInsights.

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
	"fmt"

	"github.com/RedHatInsights/rhc-osdk-utils/utils"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// configAppName is the valpop "app" (-r) under which the generated config assets are
// uploaded. Treating config as just another valpop app reuses the exact pushcache
// upload machinery (versioning, retention, S3 layout) for free.
const configAppName = "config"

// configAssetsMountPath is where the generated config ConfigMap is mounted inside
// the valpop container so valpop can read the asset files and upload them to S3.
const configAssetsMountPath = "/config"

// generateConfigAssetsJobName returns the env-scoped name for the config upload job.
// The job is a singleton per (environment, namespace): every Frontend in the same
// namespace converges on this one name, so concurrent fan-out reconciles do not
// create duplicates.
func (r *FrontendReconciliation) generateConfigAssetsJobName() string {
	return r.Frontend.Spec.EnvName + "-config-assets"
}

// isConfigAssetsJobCurrent reports whether an existing job already reflects the
// current config content, valpop image, and cutoff timestamp. If any differ, the
// job is stale and must be recreated (Jobs are immutable, so we delete + recreate).
func (r *FrontendReconciliation) isConfigAssetsJobCurrent(j *batchv1.Job, configHash string) bool {
	ann := j.Spec.Template.ObjectMeta.Annotations
	if ann["config-hash"] != configHash {
		return false
	}
	if ann["valpop-image"] != r.FrontendEnvironment.Spec.ValpopImage {
		return false
	}

	cutoff := r.FrontendEnvironment.Spec.DeployCutoffTimestampConfigAssets
	if cutoff != "" {
		jobTimestamp := j.CreationTimestamp.Format("2006-01-02T15:04:05Z")
		if jobTimestamp < cutoff {
			return false
		}
	}

	return true
}

// reconcileConfigAssetsJob manages the env-level valpop Job that uploads the
// operator-generated config assets to S3. It is driven by configHash (a fingerprint
// of the generated ConfigMap data) so a new upload is only triggered when the
// uploaded content actually changes.
//
// It is intentionally NOT gated by DisableContainerDeployments: the whole point of
// the new frontend-config flow is to publish config in clusters that run no
// per-Frontend Deployments (and therefore no clowder/chrome-service dependency).
func (r *FrontendReconciliation) reconcileConfigAssetsJob(configHash string) error {
	jobName := r.generateConfigAssetsJobName()
	nn := types.NamespacedName{Name: jobName, Namespace: r.Frontend.Namespace}

	existing := &batchv1.Job{}
	exists := true
	if err := r.Client.Get(r.Ctx, nn, existing); err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
		exists = false
	}

	// Feature disabled: tear down any previously created job and stop.
	if !r.FrontendEnvironment.Spec.EnableConfigAssets {
		if exists && existing.GetDeletionTimestamp().IsZero() {
			return r.deleteConfigAssetsJob(existing)
		}
		return nil
	}

	valpopImage := r.FrontendEnvironment.Spec.ValpopImage
	if valpopImage == "" {
		return fmt.Errorf("ValpopImage must be specified in the FrontendEnvironment when EnableConfigAssets is enabled")
	}

	if exists {
		// A terminating job cannot be recreated under the same name; requeue.
		if !existing.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("config assets job %s is terminating, will retry", jobName)
		}
		// Content, image, and cutoff all match: nothing to upload.
		if r.isConfigAssetsJobCurrent(existing, configHash) {
			return nil
		}
		// Stale: Jobs are immutable, so delete now. The job is owned by the
		// FrontendEnvironment and therefore not covered by the controller's Owns(Job)
		// watch, so its deletion will not re-enqueue a reconcile on its own. Return a
		// retry error (as pushcache does for terminating jobs) to requeue and recreate
		// it with fresh content.
		if err := r.deleteConfigAssetsJob(existing); err != nil {
			return err
		}
		return fmt.Errorf("config assets job %s is stale, deleted and will be recreated on retry", jobName)
	}

	job, err := r.buildConfigAssetsJob(nn, configHash, valpopImage)
	if err != nil {
		return err
	}

	if err := r.Client.Create(r.Ctx, job); err != nil {
		// Concurrent fan-out reconciles may race to create the singleton job.
		if k8serr.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	r.Log.Info("Created config assets upload job", "name", jobName, "namespace", nn.Namespace, "configHash", configHash)
	return nil
}

func (r *FrontendReconciliation) deleteConfigAssetsJob(j *batchv1.Job) error {
	backgroundDeletion := metav1.DeletePropagationBackground
	return r.Client.Delete(r.Ctx, j, &client.DeleteOptions{PropagationPolicy: &backgroundDeletion})
}

// buildConfigAssetsJob constructs the valpop upload Job. The generated config
// ConfigMap (named after the environment) is mounted read-only at the source path;
// valpop uploads its files under the "config" app, reusing the same S3 layout and
// retention behaviour as the per-Frontend pushcache jobs.
func (r *FrontendReconciliation) buildConfigAssetsJob(nn types.NamespacedName, configHash, valpopImage string) (*batchv1.Job, error) {
	objectStore, err := getObjectStoreConfig(r.Ctx, r.Client, r.Frontend.Namespace)
	if err != nil {
		return nil, err
	}

	bucketName := *objectStore.Name
	awsUsername := *objectStore.AccessKey
	awsPassword := *objectStore.SecretKey
	hostname := *objectStore.Endpoint
	port := *objectStore.Port

	// Minimum asset records to keep (default: 3), shared with the pushcache setting.
	minAssetRecords := 3
	if r.FrontendEnvironment.Spec.MinAssetRecords != nil {
		minAssetRecords = *r.FrontendEnvironment.Spec.MinAssetRecords
	}

	// Identical invocation to populatePushCacheContainer, with the app (-r) set to
	// "config" and the source (-s) pointing at the mounted config ConfigMap. There is
	// no source frontend image, so -i is set to the valpop image (a stable, valid image
	// reference) purely as valpop's version key.
	command := fmt.Sprintf(
		"valpop populate -r %s -s %s -i %s --valpop-image %s --timeout 172800 --min-asset-records %d --bucket %s --hostname %s --port %s --username %s --password %s",
		configAppName, configAssetsMountPath, valpopImage, valpopImage, minAssetRecords, bucketName, hostname, port, awsUsername, awsPassword,
	)

	labels := map[string]string{
		"frontendenv": r.Frontend.Spec.EnvName,
		"component":   "frontend-config",
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nn.Name,
			Namespace: nn.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Completions: utils.Int32Ptr(1),
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"config-hash":               configHash,
						"valpop-image":              valpopImage,
						"kube-linter.io/ignore-all": "we don't need no any checking",
					},
				},
				Spec: v1.PodSpec{
					RestartPolicy: v1.RestartPolicyNever,
					Volumes: []v1.Volume{{
						Name: "config-volume",
						VolumeSource: v1.VolumeSource{
							ConfigMap: &v1.ConfigMapVolumeSource{
								LocalObjectReference: v1.LocalObjectReference{Name: r.Frontend.Spec.EnvName},
							},
						},
					}},
					Containers: []v1.Container{{
						Name:    "valpop-config",
						Image:   valpopImage,
						Command: []string{"/bin/bash", "-c", command},
						VolumeMounts: []v1.VolumeMount{{
							Name:      "config-volume",
							MountPath: configAssetsMountPath,
						}},
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("100m"),
								v1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("200m"),
								v1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					}},
				},
			},
		},
	}

	// Owned by the (cluster-scoped) FrontendEnvironment, mirroring the generated
	// ConfigMaps, so the job is not garbage-collected when a single Frontend is deleted.
	job.SetOwnerReferences([]metav1.OwnerReference{r.FrontendEnvironment.MakeOwnerReference()})

	return job, nil
}
