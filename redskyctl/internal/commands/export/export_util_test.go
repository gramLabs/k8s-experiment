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

package export_test

import (
	"io/ioutil"
	"os"
	"testing"

	app "github.com/redskyops/redskyops-controller/api/apps/v1alpha1"
	redsky "github.com/redskyops/redskyops-controller/api/v1beta1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func createTempExperimentFile(t *testing.T) (*redsky.Experiment, []byte, *os.File) {
	samplePatch := `spec:
   template:
     spec:
       containers:
         - name: postgres
           resources:
             limits:
               cpu: "{{ .Values.cpu }}m"
               memory: "{{ .Values.memory }}Mi"
             requests:
               cpu: "{{ .Values.cpu }}m"
               memory: "{{ .Values.memory }}Mi"`

	tm := &metav1.TypeMeta{}
	tm.SetGroupVersionKind(redsky.GroupVersion.WithKind("Experiment"))
	sampleExperiment := &redsky.Experiment{
		TypeMeta: *tm,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sampleExperiment",
			Namespace: "default",
		},
		Spec: redsky.ExperimentSpec{
			Parameters: []redsky.Parameter{
				{
					Name: "myparam",
					Min:  100,
					Max:  1000,
				},
			},
			Metrics: []redsky.Metric{},
			Patches: []redsky.PatchTemplate{
				{
					TargetRef: &corev1.ObjectReference{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
						Name:       "postgres",
					},
					Patch: samplePatch,
				},
			},
			TrialTemplate: redsky.TrialTemplateSpec{},
		},
		Status: redsky.ExperimentStatus{},
	}

	tmpfile, err := ioutil.TempFile("", "experiment-*.yaml")
	require.NoError(t, err)

	b, err := yaml.Marshal(sampleExperiment)
	require.NoError(t, err)

	_, err = tmpfile.Write(b)
	require.NoError(t, err)

	return sampleExperiment, b, tmpfile
}

func createTempManifests(t *testing.T) *os.File {
	tmpfile, err := ioutil.TempFile("", "manifest-*.yaml")
	require.NoError(t, err)

	_, err = tmpfile.Write(pgDeployment)
	require.NoError(t, err)

	return tmpfile
}

func createTempApplication(t *testing.T, filename string) (*app.Application, []byte, *os.File) {
	tm := &metav1.TypeMeta{}
	tm.SetGroupVersionKind(app.GroupVersion.WithKind("Application"))
	sampleApplication := &app.Application{
		TypeMeta: *tm,
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sampleApplication",
			Namespace: "default",
		},
		Resources: []string{filename},
		Parameters: &app.Parameters{
			ContainerResources: &app.ContainerResources{
				Labels: map[string]string{
					"component": "postgres",
				},
			},
		},
		Scenarios: []app.Scenario{
			{
				Name: "how-do-you-make-a-tissue-dance",
				StormForger: &app.StormForgerScenario{
					TestCase: "tissue-box",
				},
			},
			{
				Name: "put-a-little-boogie-in-it",
				StormForger: &app.StormForgerScenario{
					TestCase: "boogie",
				},
			},
		},
		Objectives: []app.Objective{
			{
				Name: "cost",
				Max:  resource.NewQuantity(100, resource.DecimalExponent),
				Requests: &app.RequestsObjective{
					Labels: map[string]string{"everybody": "yes"},
					Weights: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100M"),
					},
				},
			},
			// v1alpha1.Application.Resources: []string: Objectives: []v1alpha1.Objective: v1alpha1.Objective.Name: Max: Latency: unmarshalerDecoder: json: cannot unmarshal object into Go value of type v1alpha1.LatencyType, error found in #10 byte of ...|ntile_99"},"max":"50|..., bigger context ...|00M"}}},{"latency":{"LatencyType":"percentile_99"},"max":"50","name":"latency"}],"resources":["/var/|...
			// {
			// 	Name: "latency",
			// 	Max:  resource.NewQuantity(50, resource.DecimalExponent),
			// 	Latency: &app.LatencyObjective{
			// 		LatencyType: app.LatencyPercentile99,
			// 	},
			// },
		},
		StormForger: &app.StormForger{
			Organization: "gotta",
			AccessToken: &app.StormForgerAccessToken{
				Literal: "get down!",
			},
		},
	}

	tmpfile, err := ioutil.TempFile("", "application-*.yaml")
	require.NoError(t, err)

	b, err := yaml.Marshal(sampleApplication)
	require.NoError(t, err)

	_, err = tmpfile.Write(b)
	require.NoError(t, err)

	return sampleApplication, b, tmpfile
}

// hack to allow offline testing
// built from kustomize build $recipes/postgres/application
var pgDeployment = []byte(`---
apiVersion: v1
kind: Secret
metadata:
  name: postgres-secret
stringData:
  PG_DATABASE: test_db
  PG_HOSTNAME: postgres
  PG_PASSWORD: test_password
  PG_USERNAME: test_user
  PGBENCH_CLIENTS: "1"
  PGBENCH_JOBS: "1"
  PGBENCH_SCALE: "20"
  PGBENCH_TRANSACTIONS: "1"
---
apiVersion: v1
kind: Service
metadata:
  labels:
    component: postgres
  name: postgres
spec:
  ports:
  - port: 5432
  selector:
    component: postgres
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    component: postgres
  name: postgres
spec:
  selector:
    matchLabels:
      component: postgres
  template:
    metadata:
      labels:
        component: postgres
    spec:
      containers:
      - env:
        - name: PGDATA
          value: /var/lib/postgresql/data/pgdata
        - name: POSTGRES_DB
          valueFrom:
            secretKeyRef:
              key: PG_DATABASE
              name: postgres-secret
        - name: POSTGRES_USER
          valueFrom:
            secretKeyRef:
              key: PG_USERNAME
              name: postgres-secret
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              key: PG_PASSWORD
              name: postgres-secret
        image: postgres:11.1-alpine
        livenessProbe:
          exec:
            command:
            - pg_isready
            - -h
            - localhost
            - -U
            - test_user
            - -d
            - test_db
          initialDelaySeconds: 10
          periodSeconds: 5
        name: postgres
        ports:
        - containerPort: 5432
          name: postgres
        readinessProbe:
          initialDelaySeconds: 15
          periodSeconds: 5
          tcpSocket:
            port: 5432
        resources:
          limits:
            cpu: 1
            memory: 2000Mi
          requests:
            cpu: 1
            memory: 2000Mi
        securityContext:
          allowPrivilegeEscalation: false
          runAsUser: 70
        volumeMounts:
        - mountPath: /var/lib/postgresql/data
          name: postgres-storage
      volumes:
      - emptyDir: {}
        name: postgres-storage`)
