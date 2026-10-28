/*
Copyright The Kubernetes Authors.

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

package kubeconfig

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
)

// a minimal kubeadm admin.conf that passes checkKubeadmExpectations
// (exactly one cluster, one user, one context)
const validAdminConf = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: definitelyacert
    server: https://192.168.9.4:6443
  name: kind
contexts:
- context:
    cluster: kind
    user: kubernetes-admin
  name: kubernetes-admin@kind
current-context: kubernetes-admin@kind
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: seemslegit
    client-key-data: yep
`

// valid YAML, but two clusters, so it fails checkKubeadmExpectations
const twoClusterAdminConf = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: definitelyacert
    server: https://192.168.9.4:6443
  name: kind
- cluster:
    certificate-authority-data: definitelyacert
    server: https://192.168.9.5:6443
  name: kind2
contexts:
- context:
    cluster: kind
    user: kubernetes-admin
  name: kubernetes-admin@kind
current-context: kubernetes-admin@kind
kind: Config
preferences: {}
users:
- name: kubernetes-admin
  user:
    client-certificate-data: seemslegit
    client-key-data: yep
`

// fakeCmd implements exec.Cmd. Run() either writes content to the captured
// stdout writer or returns err.
type fakeCmd struct {
	content string
	err     error
	stdout  io.Writer
}

func (c *fakeCmd) Run() error {
	if c.err != nil {
		return c.err
	}
	if c.stdout != nil {
		_, _ = io.WriteString(c.stdout, c.content)
	}
	return nil
}
func (c *fakeCmd) SetEnv(...string) exec.Cmd      { return c }
func (c *fakeCmd) SetStdin(io.Reader) exec.Cmd    { return c }
func (c *fakeCmd) SetStdout(w io.Writer) exec.Cmd { c.stdout = w; return c }
func (c *fakeCmd) SetStderr(io.Writer) exec.Cmd   { return c }

// fakeNode implements nodes.Node. Its only meaningful behavior is Command,
// which returns a fakeCmd reproducing this node's admin.conf read; commandCount
// records how many commands were issued so tests can assert a node was skipped.
type fakeNode struct {
	name         string
	content      string
	err          error
	commandCount int
}

func (n *fakeNode) String() string              { return n.name }
func (n *fakeNode) Role() (string, error)       { return "control-plane", nil }
func (n *fakeNode) IP() (string, string, error) { return "", "", nil }
func (n *fakeNode) SerialLogs(io.Writer) error  { return nil }
func (n *fakeNode) Command(string, ...string) exec.Cmd {
	n.commandCount++
	return &fakeCmd{content: n.content, err: n.err}
}
func (n *fakeNode) CommandContext(context.Context, string, ...string) exec.Cmd {
	n.commandCount++
	return &fakeCmd{content: n.content, err: n.err}
}

func toNodeList(ns ...*fakeNode) []nodes.Node {
	out := make([]nodes.Node, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}

func TestKubeConfigFromControlPlanes(t *testing.T) {
	t.Parallel()

	t.Run("first node works, others untouched", func(t *testing.T) {
		t.Parallel()
		cp1 := &fakeNode{name: "c-control-plane", content: validAdminConf}
		cp2 := &fakeNode{name: "c-control-plane2", content: validAdminConf}
		cfg, err := kubeConfigFromControlPlanes(toNodeList(cp1, cp2), "c")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected a config, got nil")
		}
		if cp1.commandCount != 1 {
			t.Errorf("first node should be read once, was read %d times", cp1.commandCount)
		}
		if cp2.commandCount != 0 {
			t.Errorf("second node should not be contacted when the first works, was read %d times", cp2.commandCount)
		}
	})

	t.Run("first node read fails, second works", func(t *testing.T) {
		t.Parallel()
		cp1 := &fakeNode{name: "c-control-plane", err: errors.New("exit status 1")}
		cp2 := &fakeNode{name: "c-control-plane2", content: validAdminConf}
		cfg, err := kubeConfigFromControlPlanes(toNodeList(cp1, cp2), "c")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected a config from the second node, got nil")
		}
		if cp2.commandCount != 1 {
			t.Errorf("second node should have been read once, was read %d times", cp2.commandCount)
		}
	})

	t.Run("first node fails expectations, second works", func(t *testing.T) {
		t.Parallel()
		cp1 := &fakeNode{name: "c-control-plane", content: twoClusterAdminConf}
		cp2 := &fakeNode{name: "c-control-plane2", content: validAdminConf}
		cfg, err := kubeConfigFromControlPlanes(toNodeList(cp1, cp2), "c")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg == nil {
			t.Fatal("expected a config from the second node, got nil")
		}
	})

	t.Run("every node fails names each node", func(t *testing.T) {
		t.Parallel()
		cp1 := &fakeNode{name: "c-control-plane", err: errors.New("boom1")}
		cp2 := &fakeNode{name: "c-control-plane2", err: errors.New("boom2")}
		cp3 := &fakeNode{name: "c-control-plane3", err: errors.New("boom3")}
		cfg, err := kubeConfigFromControlPlanes(toNodeList(cp1, cp2, cp3), "c")
		if err == nil {
			t.Fatal("expected an error when every node fails")
		}
		if cfg != nil {
			t.Error("expected nil config on total failure")
		}
		msg := err.Error()
		if !strings.Contains(msg, "failed to get cluster internal kubeconfig") {
			t.Errorf("error should keep the historical prefix, got: %v", msg)
		}
		// check the %q-quoted forms so that "c-control-plane" is not matched
		// merely by being a substring of "c-control-plane2"
		for _, name := range []string{"c-control-plane", "c-control-plane2", "c-control-plane3"} {
			quoted := strconv.Quote(name)
			if !strings.Contains(msg, quoted) {
				t.Errorf("error should name node %s, got: %v", quoted, msg)
			}
		}
	})

	t.Run("single control-plane failure keeps legacy prefix", func(t *testing.T) {
		t.Parallel()
		cp1 := &fakeNode{name: "c-control-plane", err: errors.New("exit status 1")}
		_, err := kubeConfigFromControlPlanes(toNodeList(cp1), "c")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "failed to get cluster internal kubeconfig") {
			t.Errorf("error should contain the historical prefix, got: %v", err.Error())
		}
	})
}
