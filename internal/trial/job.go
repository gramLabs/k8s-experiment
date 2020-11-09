/*
Copyright 2020 GramLabs, Inc.

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

package trial

import (
	"encoding/json"
	"fmt"
	"time"

	redskyv1beta1 "github.com/redskyops/redskyops-controller/api/v1beta1"
	"github.com/redskyops/redskyops-controller/internal/meta"
	"github.com/redskyops/redskyops-controller/internal/setup"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// NewJob returns a new trial run job from the template on the trial
func NewJob(t *redskyv1beta1.Trial) *batchv1.Job {
	job := batchv1.Job{}
	if t.Spec.JobTemplate != nil {
		// Copy the job template into the new job
		t.Spec.JobTemplate.ObjectMeta.DeepCopyInto(&job.ObjectMeta)
		t.Spec.JobTemplate.Spec.DeepCopyInto(&job.Spec)
	} else {
		// If there is no job template, make sure we at least get the experiment name labels
		meta.AddLabel(&job, redskyv1beta1.LabelExperiment, t.ExperimentNamespacedName().Name)
		meta.AddLabel(&job.Spec.Template, redskyv1beta1.LabelExperiment, t.ExperimentNamespacedName().Name)
	}

	// Apply labels
	meta.AddLabel(&job, redskyv1beta1.LabelTrial, t.Name)
	meta.AddLabel(&job.Spec.Template, redskyv1beta1.LabelTrial, t.Name)
	meta.AddLabel(&job, redskyv1beta1.LabelTrialRole, "trialRun")
	meta.AddLabel(&job.Spec.Template, redskyv1beta1.LabelTrialRole, "trialRun")

	// Provide default metadata
	job.Namespace = t.Namespace
	if job.Name == "" {
		job.Name = t.Name
	}

	// The default restart policy for a pod is not acceptable in the context of a job
	if job.Spec.Template.Spec.RestartPolicy == "" {
		job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	}

	// The default backoff limit will restart the trial job which is unlikely to produce desirable results
	if job.Spec.BackoffLimit == nil {
		job.Spec.BackoffLimit = new(int32)
	}

	// Expose the current assignments as environment variables to every container (except the default sleep container added below)
	for i := range job.Spec.Template.Spec.Containers {
		c := &job.Spec.Template.Spec.Containers[i]
		c.Env = setup.AppendAssignmentEnv(t, c.Env)
		c.Env = setup.AppendPrometheusEnv(t, c.Env)
	}

	// Containers cannot be empty, inject a sleep by default
	if len(job.Spec.Template.Spec.Containers) == 0 {
		addDefaultContainer(t, &job)
	}

	// Check to see if there is patch for the (as of yet, non-existent) trial job
	return patchSelf(t, &job)
}

func addDefaultContainer(t *redskyv1beta1.Trial, job *batchv1.Job) {
	// Determine the sleep time
	s := t.Spec.ApproximateRuntime
	if s == nil || s.Duration == 0 {
		s = &metav1.Duration{Duration: 2 * time.Minute}
	}
	if t.Spec.StartTimeOffset != nil {
		s = &metav1.Duration{Duration: s.Duration + t.Spec.StartTimeOffset.Duration}
	}

	// Add a busybox container that just runs sleep
	job.Spec.Template.Spec.Containers = []corev1.Container{
		{
			Name:    "default-trial-run",
			Image:   "busybox",
			Command: []string{"/bin/sh"},
			Args:    []string{"-c", fmt.Sprintf("echo 'Sleeping for %s...' && sleep %.0f && echo 'Done.'", s.Duration.String(), s.Seconds())},
		},
	}
}

func patchSelf(t *redskyv1beta1.Trial, job *batchv1.Job) *batchv1.Job {
	// Look for patch operations that match this trial and apply them
	for i := range t.Status.PatchOperations {
		po := &t.Status.PatchOperations[i]
		if IsTrialJobReference(t, &po.TargetRef) && po.PatchType == types.StrategicMergePatchType {
			// Ignore errors all the way down, only overwrite the job if everything is successful
			if original, err := json.Marshal(job); err == nil {
				j := &batchv1.Job{}
				if patched, err := strategicpatch.StrategicMergePatch(original, po.Data, j); err == nil {
					if err := json.Unmarshal(patched, j); err == nil {
						return j
					}
				}
			}
		}
	}
	return job
}
