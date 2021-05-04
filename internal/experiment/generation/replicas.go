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

package generation

import (
	redskyv1beta1 "github.com/thestormforge/optimize-controller/api/v1beta1"
	"github.com/thestormforge/optimize-controller/internal/scan"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// ReplicaSelector identifies zero or more replica specifications.
type ReplicaSelector struct {
	scan.GenericSelector
	// Path to the replica field.
	Path string `json:"path,omitempty"`
	// Create container resource specifications even if the original object does not contain them.
	CreateIfNotPresent bool `json:"create,omitempty"`
}

var _ scan.Selector = &ReplicaSelector{}

func (s *ReplicaSelector) Default() {
	if s.Kind == "" {
		s.Group = "apps|extensions"
		s.Kind = "Deployment|StatefulSet"
	}
	if s.Path == "" {
		s.Path = "/spec/replicas"
	}
}

func (s *ReplicaSelector) Map(node *yaml.RNode, meta yaml.ResourceMeta) ([]interface{}, error) {
	var result []interface{}

	path := splitPath(s.Path)
	err := node.PipeE(
		&yaml.PathGetter{Path: path, Create: yaml.ScalarNode},
		yaml.FilterFunc(func(node *yaml.RNode) (*yaml.RNode, error) {
			if node.YNode().Value == "" && !s.CreateIfNotPresent {
				return node, nil
			}

			result = append(result, &replicaParameter{pnode: pnode{
				meta:      meta,
				fieldPath: node.FieldPath(),
				value:     node.YNode(),
			}})

			return node, nil
		}))

	if err != nil {
		return nil, err
	}

	return result, nil
}

type replicaParameter struct {
	pnode
}

var _ PatchSource = &replicaParameter{}
var _ ParameterSource = &replicaParameter{}

func (p *replicaParameter) Patch(name ParameterNamer) (yaml.Filter, error) {
	value := yaml.NewScalarRNode("{{ .Values." + name(p.meta, p.fieldPath, "replicas") + " }}")
	value.YNode().Tag = yaml.NodeTagInt
	return yaml.Tee(
		&yaml.PathGetter{Path: p.fieldPath, Create: yaml.ScalarNode},
		yaml.FieldSetter{Value: value, OverrideStyle: true},
	), nil
}

func (p *replicaParameter) Parameters(name ParameterNamer) ([]redskyv1beta1.Parameter, error) {
	var v int
	if err := p.value.Decode(&v); err != nil {
		return nil, err
	}
	if v <= 0 {
		return nil, nil
	}

	baselineReplicas := intstr.FromInt(v)
	var minReplicas, maxReplicas int32 = 1, 5

	// Only adjust the max replica count if necessary
	if baselineReplicas.IntVal > maxReplicas {
		maxReplicas = baselineReplicas.IntVal
	}

	return []redskyv1beta1.Parameter{{
		Name:     name(p.meta, p.fieldPath, "replicas"),
		Min:      minReplicas,
		Max:      maxReplicas,
		Baseline: &baselineReplicas,
	}}, nil
}
