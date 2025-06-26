/*
   Copyright The runm Authors.

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

package schedcore

import (
	"context"

	"github.com/containerd/containerd/v2/pkg/schedcore"

	runmv1 "github.com/walteh/runm/proto/v1"
)

var _ runmv1.TTRPCPrctlServiceService = (*PrctlServer)(nil)

type PrctlServer struct {
}

// Create a new sched core domain
func (s *PrctlServer) Create(ctx context.Context, req *runmv1.CreateRequest) (*runmv1.CreateResponse, error) {
	goError := schedcore.Create(schedcore.PidType(req.GetPidType()))

	createResponse := runmv1.NewCreateResponse(&runmv1.CreateResponse_builder{
		GoError: goError.Error(),
	})

	return createResponse, nil
}

// ShareFrom shares the sched core domain from the provided pid
func (s *PrctlServer) ShareFrom(ctx context.Context, req *runmv1.ShareFromRequest) (*runmv1.ShareFromResponse, error) {
	goError := schedcore.ShareFrom(req.GetPid(), schedcore.PidType(req.GetPidType()))
	shareFromResponse := runmv1.NewShareFromResponse(&runmv1.ShareFromResponse_builder{
		GoError: goError.Error(),
	})

	return shareFromResponse, nil
}
