package stackerr

import (
	"encoding/json"

	"gitlab.com/tozd/go/errors"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

// EnhancedSourceErrorWithMessage wraps any error and carries an
// (optional) Source pointer plus a human‐readable Message.
type StackedEncodableError struct {
	Source  *EnhancedSource        `json:"source"`
	Message string                 `json:"message"`
	Next    *StackedEncodableError `json:"next"`
}

func (e *StackedEncodableError) Error() string {
	return e.Message
}

func (e *StackedEncodableError) Unwrap() error {
	return e.Next
}

// NewFromError builds a linked list of EnhancedSourceErrorWithMessage
// nodes, one per layer in err’s chain.  If err is nil, returns nil.
func NewStackedEncodableErrorFromError(err error) *StackedEncodableError {

	if err == nil {
		return nil
	}

	if st, ok := status.FromError(err); ok {
		err = FromGRPCStatusError(st)
	}

	if enc, ok := err.(*StackedEncodableError); ok {
		return enc
	}

	var enhancedSource *EnhancedSource
	if ez, ok := err.(interface {
		Frame() uintptr
	}); ok {
		enhancedSource = NewEnhancedSource(ez.Frame())
	}

	// encz := GetEnhancedSourcesFromError(err)

	root := &StackedEncodableError{
		Message: err.Error(),
		Source:  enhancedSource,
		Next:    NewStackedEncodableErrorFromError(errors.Unwrap(err)),
	}

	// for _, enc := range encz {
	// 	root = &StackedEncodableError{
	// 		Source:  enc,
	// 		Message: "",
	// 		Next:    root,
	// 	}
	// }

	return root
}

func FromGRPCStatusError(errz *status.Status) error {
	if errz == nil {
		return nil
	}

	for _, detail := range errz.Details() {
		if di, ok := detail.(*errdetails.ErrorInfo); ok {
			if encoded, ok := di.Metadata["encoded_stack_error"]; ok {
				var se StackedEncodableError
				err := json.Unmarshal([]byte(encoded), &se)
				if err != nil {
					return errz.Err()
				}
				return &se
			}
		}
	}
	// No DebugInfo present: return the original status error
	return errz.Err()
}
