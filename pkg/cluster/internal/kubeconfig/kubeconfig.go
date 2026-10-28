/*
Copyright 2019 The Kubernetes Authors.

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

// Package kubeconfig provides utilities kind uses internally to manage
// kind cluster kubeconfigs
package kubeconfig

import (
	"bytes"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
	"sigs.k8s.io/kind/pkg/errors"

	// this package has slightly more generic kubeconfig helpers
	// and minimal dependencies on the rest of kind
	"sigs.k8s.io/kind/pkg/cluster/internal/kubeconfig/internal/kubeconfig"
	"sigs.k8s.io/kind/pkg/cluster/internal/providers"
)

// Export exports the kubeconfig given the cluster context and a path to write it to
// This will always be an external kubeconfig
func Export(p providers.Provider, name, explicitPath string, external bool) error {
	cfg, err := get(p, name, external)
	if err != nil {
		return err
	}
	return kubeconfig.WriteMerged(cfg, explicitPath)
}

// Remove removes clusterName from the kubeconfig paths detected based on
// either explicitPath being set or $KUBECONFIG or $HOME/.kube/config, following
// the rules set by kubectl
// clusterName must identify a kind cluster.
func Remove(clusterName, explicitPath string) error {
	return kubeconfig.RemoveKIND(clusterName, explicitPath)
}

// Get returns the kubeconfig for the cluster
// external controls if the internal IP address is used or the host endpoint
func Get(p providers.Provider, name string, external bool) (string, error) {
	cfg, err := get(p, name, external)
	if err != nil {
		return "", err
	}
	b, err := kubeconfig.Encode(cfg)
	if err != nil {
		return "", err
	}
	return string(b), err
}

// ContextForCluster returns the context name for a kind cluster based on
// its name. This key is used for all list entries of kind clusters
func ContextForCluster(kindClusterName string) string {
	return kubeconfig.KINDClusterKey(kindClusterName)
}

func get(p providers.Provider, name string, external bool) (*kubeconfig.Config, error) {
	// find the control plane nodes to read the kubeadm config from
	allNodes, err := p.ListNodes(name)
	if err != nil {
		return nil, err
	}
	cpNodes, err := nodeutils.ControlPlaneNodes(allNodes)
	if err != nil {
		return nil, err
	}
	if len(cpNodes) < 1 {
		return nil, errors.Errorf("could not locate any control plane nodes for cluster named '%s'. "+
			"Use the --name option to select a different cluster", name)
	}

	cfg, err := kubeConfigFromControlPlanes(cpNodes, name)
	if err != nil {
		return nil, err
	}

	// if we're doing external we need to override the server endpoint
	if external {
		endpoint, err := p.GetAPIServerEndpoint(name)
		if err != nil {
			return nil, err
		}
		cfg.Clusters[0].Cluster.Server = "https://" + endpoint
	}
	return cfg, nil
}

// kubeConfigFromControlPlanes returns the kind kubeconfig derived from the first
// control-plane node whose admin.conf can be read and parsed.
//
// Nodes are tried in the given order, so the bootstrap control-plane node (first
// after ControlPlaneNodes' sort) is preferred and a healthy cluster behaves
// exactly as before: the first node is used and no other node is contacted. A
// node whose container is down, or whose admin.conf fails checkKubeadmExpectations,
// is skipped so the kubeconfig can still be produced from another control-plane
// (the secondary nodes join with `kubeadm join --control-plane` and hold an
// equally valid admin.conf signed by the same CA, pointing at the same
// controlPlaneEndpoint). If every node fails, the returned error names each node
// and keeps the historical "failed to get cluster internal kubeconfig" prefix.
func kubeConfigFromControlPlanes(cpNodes []nodes.Node, name string) (*kubeconfig.Config, error) {
	var errs []error
	for _, node := range cpNodes {
		var buff bytes.Buffer
		if err := node.Command("cat", "/etc/kubernetes/admin.conf").SetStdout(&buff).Run(); err != nil {
			errs = append(errs, errors.Wrapf(err, "node %q", node.String()))
			continue
		}
		cfg, err := kubeconfig.KINDFromRawKubeadm(buff.String(), name, "")
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "node %q", node.String()))
			continue
		}
		return cfg, nil
	}
	return nil, errors.Wrap(errors.NewAggregate(errs), "failed to get cluster internal kubeconfig")
}
