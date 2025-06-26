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
	"errors"

	runmv1 "github.com/walteh/runm/proto/v1"
)

type PrctlClient struct {
	service runmv1.TTRPCPrctlServiceService
}

// Create a new sched core domain
func (s *PrctlClient) Create(ctx context.Context, t runmv1.PrctlPidType) error {
	createRequest := runmv1.NewCreateRequest(&runmv1.CreateRequest_builder{
		PidType: t,
	})

	r, err := s.service.Create(ctx, createRequest)
	if err != nil {
		return err
	}

	if r.GetGoError() != "" {
		return errors.New(r.GetGoError())
	}

	return nil
}

// ShareFrom shares the sched core domain from the provided pid
func (s *PrctlClient) ShareFrom(ctx context.Context, pid uint64, t runmv1.PrctlPidType) error {
	shareFromRequest := runmv1.NewShareFromRequest(&runmv1.ShareFromRequest_builder{
		Pid:     pid,
		PidType: t,
	})

	r, err := s.service.ShareFrom(ctx, shareFromRequest)
	if err != nil {
		return err
	}

	if r.GetGoError() != "" {
		return errors.New(r.GetGoError())
	}

	return nil
}
