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

package patch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	app "github.com/redskyops/redskyops-controller/api/apps/v1alpha1"
	redsky "github.com/redskyops/redskyops-controller/api/v1beta1"
	"github.com/redskyops/redskyops-controller/internal/experiment"
	"github.com/redskyops/redskyops-controller/internal/patch"
	"github.com/redskyops/redskyops-controller/internal/server"
	"github.com/redskyops/redskyops-controller/internal/template"
	"github.com/redskyops/redskyops-controller/redskyctl/internal/commander"
	experimentctl "github.com/redskyops/redskyops-controller/redskyctl/internal/commands/generate/experiment"
	"github.com/redskyops/redskyops-controller/redskyctl/internal/kustomize"
	"github.com/redskyops/redskyops-go/pkg/config"
	experimentsapi "github.com/redskyops/redskyops-go/pkg/redskyapi/experiments/v1alpha1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/resid"
	"sigs.k8s.io/kustomize/api/types"
)

// Options are the configuration options for creating a patched experiment
type Options struct {
	// Config is the Red Sky Configuration used to generate the controller installation
	Config *config.RedSkyConfig
	// ExperimentsAPI is used to interact with the Red Sky Experiments API
	ExperimentsAPI experimentsapi.API
	// IOStreams are used to access the standard process streams
	commander.IOStreams

	inputFiles  []string
	trialNumber int
	trialName   string

	// This is used for testing
	Fs filesys.FileSystem
}

// NewCommand creates a command for performing a patch
func NewCommand(o *Options) *cobra.Command {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Llongfile)

	cmd := &cobra.Command{
		Use:   "patch",
		Short: "Create a patched manifest using trial parameters",
		Long:  "Create a patched manifest using the parameters from the specified trial",

		PreRunE: func(cmd *cobra.Command, args []string) error {
			commander.SetStreams(&o.IOStreams, cmd)

			var err error
			if o.ExperimentsAPI == nil {
				err = commander.SetExperimentsAPI(&o.ExperimentsAPI, o.Config, cmd)
			}

			return err
		},
		RunE: commander.WithContextE(o.patch),
	}

	cmd.Flags().StringSliceVar(&o.inputFiles, "file", []string{""}, "experiment and related manifests to patch, - for stdin")
	cmd.Flags().IntVar(&o.trialNumber, "trialnumber", -1, "trial number")
	cmd.Flags().StringVar(&o.trialName, "trialname", "", "trial name")

	return cmd
}

func (o *Options) patch(ctx context.Context) error {
	if o.trialName == "" {
		return fmt.Errorf("a trial name must be specified")
	}

	if err := o.readInputs(); err != nil {
		return err
	}

	exp := &redsky.Experiment{}
	appl := &app.Application{}
	resources := []string{}

	for _, keyFile := range []interface{}{exp, appl} {

		_, err := findFileType(o.Fs, keyFile)
		if err != nil {
			return err
		}

		switch keyFile.(type) {
		// case *redsky.Experiment{} is the easy case and handled for us by fundFileType
		case *app.Application:
			resources = append(resources, appl.Resources...)

			gen := experimentctl.NewGenerator(o.Fs)
			gen.Application = *appl
			gen.ContainerResourcesSelector = experimentctl.DefaultContainerResourcesSelectors()
			if gen.Application.Parameters != nil && gen.Application.Parameters.ContainerResources != nil {
				ls := labels.Set(gen.Application.Parameters.ContainerResources.Labels).String()
				for i := range gen.ContainerResourcesSelector {
					gen.ContainerResourcesSelector[i].LabelSelector = ls
				}
			}

			list, err := gen.Generate()
			if err != nil {
				return err
			}

			for idx, listItem := range list.Items {
				listBytes, err := listItem.Marshal()
				if err != nil {
					return err
				}

				assetName := fmt.Sprintf("%s%d%s", "application-assets", idx, ".yaml")
				if err := o.Fs.WriteFile(assetName, listBytes); err != nil {
					return err
				}

				resources = append(resources, assetName)

				if te, ok := list.Items[idx].Object.(*redsky.Experiment); ok {
					te.DeepCopyInto(exp)
				}
			}
		}
	}

	// look up trial from api
	trialItem, err := o.getTrialByID(ctx, exp.Name)
	if err != nil {
		return err
	}

	trial := &redsky.Trial{}
	experiment.PopulateTrialFromTemplate(exp, trial)
	server.ToClusterTrial(trial, &trialItem.TrialAssignments)

	// render patches
	var patches map[string]types.Patch
	patches, err = createKustomizePatches(exp.Spec.Patches, trial)
	if err != nil {
		return err
	}

	yamls, err := kustomize.Yamls(
		kustomize.WithFS(o.Fs),
		kustomize.WithResourceNames(resources),
		kustomize.WithPatches(patches),
	)
	if err != nil {
		return err
	}

	fmt.Fprintln(o.Out, string(yamls))

	return nil
}

func (o *Options) getTrialByID(ctx context.Context, experimentName string) (*experimentsapi.TrialItem, error) {
	query := &experimentsapi.TrialListQuery{
		Status: []experimentsapi.TrialStatus{experimentsapi.TrialCompleted},
	}

	trialList, err := o.getTrials(ctx, experimentName, query)
	if err != nil {
		return nil, err
	}

	// Cut off just the trial number from the trial name
	trialNum := o.trialName[strings.LastIndex(o.trialName, "-")+1:]
	trialNumber, err := strconv.Atoi(trialNum)
	if err != nil {
		return nil, err
	}

	// Isolate the given trial we want by number
	var wantedTrial *experimentsapi.TrialItem
	for _, trial := range trialList.Trials {
		if trial.Number == int64(trialNumber) {
			wantedTrial = &trial
			break
		}
	}

	if wantedTrial == nil {
		return nil, fmt.Errorf("trial not found")
	}

	return wantedTrial, nil
}

// getTrials gets all trials from the redsky api for a given experiment.
func (o *Options) getTrials(ctx context.Context, experimentName string, query *experimentsapi.TrialListQuery) (trialList experimentsapi.TrialList, err error) {
	if o.ExperimentsAPI == nil {
		return trialList, fmt.Errorf("unable to connect to api server")
	}

	experiment, err := o.ExperimentsAPI.GetExperimentByName(ctx, experimentsapi.NewExperimentName(experimentName))
	if err != nil {
		return trialList, err
	}

	if experiment.TrialsURL == "" {
		return trialList, fmt.Errorf("unable to identify trial")
	}

	return o.ExperimentsAPI.GetAllTrials(ctx, experiment.TrialsURL, query)
}

// readInputs handles all of the loading of files and/or stdin. It utilizes kio.pipelines
// so we can better handle reading from stdin and getting at the specific data we need.
func (o *Options) readInputs() error {
	if o.Fs == nil {
		o.Fs = filesys.MakeFsInMemory()
	}

	for _, filename := range o.inputFiles {
		r, err := o.IOStreams.OpenFile(filename)
		if err != nil {
			return err
		}
		defer r.Close()

		// Read the input files into a buffer so we can account for reading
		// from StdIn
		var buf bytes.Buffer
		if _, err = buf.ReadFrom(r); err != nil {
			return err
		}

		if filename == "-" {
			filename = "stdin"
		}

		if err := o.Fs.WriteFile(filepath.Base(filename), buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

// createKustomizePatches translates a patchTemplate into a kustomize (json) patch
func createKustomizePatches(patchSpec []redsky.PatchTemplate, trial *redsky.Trial) (map[string]types.Patch, error) {
	te := template.New()
	patches := map[string]types.Patch{}

	for idx, expPatch := range patchSpec {
		ref, data, err := patch.RenderTemplate(te, trial, &expPatch)
		if err != nil {
			return nil, err
		}

		// Surely there's got to be a better way
		// // Transition patch from json to map[string]interface
		m := make(map[string]interface{})
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}

		u := &unstructured.Unstructured{}
		// // Set patch data first ( otherwise it overwrites everything else )
		u.SetUnstructuredContent(m)
		// // Define object/type meta
		u.SetName(ref.Name)
		u.SetNamespace(ref.Namespace)
		u.SetGroupVersionKind(ref.GroupVersionKind())
		// // Profit
		b, err := u.MarshalJSON()
		if err != nil {
			return nil, err
		}

		patches[fmt.Sprintf("%s-%d", "patch", idx)] = types.Patch{
			Patch: string(b),
			Target: &types.Selector{
				Gvk: resid.Gvk{
					Group:   ref.GroupVersionKind().Group,
					Version: ref.GroupVersionKind().Version,
					Kind:    ref.GroupVersionKind().Kind,
				},
				Name:      ref.Name,
				Namespace: ref.Namespace,
			},
		}
	}

	return patches, nil
}

func findFileType(fs filesys.FileSystem, ft interface{}) ([]string, error) {
	filenames := []string{}
	walkFn := func(path string, info os.FileInfo, err error) error {
		//log.Println("fs.path", path)
		if err != nil {
			return err
		}

		if fs.IsDir(path) {
			return nil
		}

		// This should account for when we pass in `struct{}`
		if _, ok := ft.(struct{}); ok {
			filenames = append(filenames, filepath.Base(path))
			return nil
		}

		// Need to do this weird double read because ioutil.ReadAll on a filesys.File
		// makes bad things happen
		data, err := fs.ReadFile(path)
		if err != nil {
			return err
		}

		if err := commander.NewResourceReader().ReadInto(ioutil.NopCloser(bytes.NewReader(data)), ft.(runtime.Object)); err != nil {
			// We're going to skip invalid errors here because its most likely due to us trying to
			// read the file into the incorrect type
			// We'll account for this later by ensuring those types are != nil
			return nil
		}

		filenames = append(filenames, filepath.Base(path))

		return nil
	}

	if err := fs.Walk("/", walkFn); err != nil {
		return filenames, err
	}

	return filenames, nil
}
