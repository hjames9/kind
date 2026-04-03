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

/*
Package layeraware implements layer-aware image loading.

This package provides functionality to load Docker images efficiently
by only transferring layers that don't already exist on the node.

Supports multiple images in a single tar archive, matching current
kind behavior.

The approach:
1. Parse tar to extract metadata for all images
2. Query node's content store for existing blobs
3. Stream missing blobs to node
4. Import original tar (ctr skips existing blobs)

Example:

	metadata, _ := layeraware.InspectArchive("/tmp/images.tar")
	plan, _ := layeraware.PlanTransfer(metadata, node)
	err := layeraware.ExecuteTransfer("/tmp/images.tar", plan, logger)
*/
package layeraware
