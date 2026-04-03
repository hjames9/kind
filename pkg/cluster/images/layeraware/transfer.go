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
	"os"

	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/log"
)

// ExecuteTransfer executes a transfer plan
//
// This:
// 1. Streams missing blobs (if any)
// 2. Imports original tar (ctr skips existing blobs)
func ExecuteTransfer(
	archivePath string,
	plan *TransferPlan,
	logger log.Logger,
) error {
	// Step 1: Stream missing blobs
	if plan.NeedsTransfer() {
		if logger != nil {
			logger.V(0).Infof("Transferring %d blobs to %s",
				len(plan.MissingBlobs),
				plan.Node.String())
		}

		err := StreamMissingBlobs(archivePath, plan.Node, plan.MissingBlobs, logger)
		if err != nil {
			return errors.Wrap(err, "failed to stream blobs")
		}
	} else {
		if logger != nil {
			logger.V(0).Infof("All blobs already exist on node %s", plan.Node.String())
		}
	}

	// Step 2: Import original tar
	if logger != nil {
		imageCount := plan.Metadata.ImageCount()
		if imageCount == 1 {
			logger.V(0).Infof("Registering image on node %s", plan.Node.String())
		} else {
			logger.V(0).Infof("Registering %d images on node %s", imageCount, plan.Node.String())
		}
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return errors.Wrap(err, "failed to open archive for import")
	}
	defer f.Close()

	err = nodeutils.LoadImageArchive(plan.Node, f)
	if err != nil {
		return errors.Wrap(err, "failed to import images")
	}

	return nil
}
