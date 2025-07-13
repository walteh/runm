//go:build darwin || freebsd || netbsd || openbsd

/*
   Copyright The containerd Authors.

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

package runm

import (
	"github.com/containerd/containerd/v2/pkg/stdio"
	"gitlab.com/tozd/go/errors"

	"github.com/walteh/runm/pkg/kqueue"
)

// NewPlatform returns a linux platform for use with I/O operations
func NewPlatform() (stdio.Platform, error) {
	kqueuer, err := kqueue.NewKqueuer()
	if err != nil {
		return nil, errors.Errorf("initializing kqueuer: %w", err)
	}
	go kqueuer.Wait()
	return newPlatform(kqueuer)
}
