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

package layeraware

import (
	"archive/tar"
	"io"
	"os"
	"path"
	"strings"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/log"
)

// StreamMissingBlobs streams blobs from tar to node without buffering
//
// Memory usage: constant (~10MB) regardless of image size
func StreamMissingBlobs(
	tarPath string,
	node nodes.Node,
	missingDigests []string,
	logger log.Logger,
) error {
	if len(missingDigests) == 0 {
		return nil
	}

	f, err := os.Open(tarPath)
	if err != nil {
		return errors.Wrap(err, "failed to open tar")
	}
	defer f.Close()

	return streamFromReader(f, node, missingDigests, logger)
}

func streamFromReader(
	r io.Reader,
	node nodes.Node,
	missingDigests []string,
	logger log.Logger,
) error {
	// Build lookup map
	remaining := make(map[string]bool)
	for _, d := range missingDigests {
		remaining[d] = true
		if strings.HasPrefix(d, "sha256:") {
			remaining[strings.TrimPrefix(d, "sha256:")] = true
		}
	}

	tr := tar.NewReader(r)
	transferred := 0
	totalToTransfer := len(missingDigests)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to read tar")
		}

		digest := digestFromPath(hdr.Name)
		if digest == "" {
			continue
		}

		normalizedDigest := digest
		if !strings.HasPrefix(digest, "sha256:") {
			normalizedDigest = "sha256:" + digest
		}

		if !remaining[digest] && !remaining[normalizedDigest] {
			// Skip blobs we don't need
			_, err = io.Copy(io.Discard, tr)
			if err != nil {
				return errors.Wrap(err, "failed to skip blob")
			}
			continue
		}

		// Stream to containerd
		if logger != nil {
			transferred++
			logger.V(0).Infof("Transferring blob %d/%d",
				transferred, totalToTransfer)
		}

		// CRITICAL: Stream directly (no buffering)
		err = nodeutils.IngestBlob(node, normalizedDigest, hdr.Size, tr)
		if err != nil {
			return errors.Wrapf(err, "failed to ingest blob %s", normalizedDigest)
		}

		delete(remaining, digest)
		delete(remaining, normalizedDigest)

		if len(remaining) == 0 {
			break
		}
	}

	// Verify
	if len(remaining) > 0 {
		missing := []string{}
		for d := range remaining {
			if strings.HasPrefix(d, "sha256:") {
				missing = append(missing, d)
			}
		}
		if len(missing) > 0 {
			return errors.Errorf("blobs not found in tar: %v", missing)
		}
	}

	return nil
}

func digestFromPath(p string) string {
	if strings.HasPrefix(p, "blobs/sha256/") {
		return path.Base(p)
	}
	base := path.Base(p)
	return strings.TrimSuffix(base, ".tar")
}
